package toolruntime

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spachava753/starlarkx/starlark"
)

func (r *runtime) resolve(path string) (string, error) {
	if path == "" {
		return "", errors.New("path must be nonempty")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(
			r.config.Cwd,
			path,
		)
	}
	return filepath.Clean(path), nil
}

func (r *runtime) readFile(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"path",
		&path,
	); err != nil {
		return nil, err
	}
	if _, err := currentInvocation(thread); err != nil {
		return nil, err
	}
	resolved, err := r.resolve(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	return starlark.Bytes(data), nil
}

func (r *runtime) writeFile(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	var data starlark.Value
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"path",
		&path,
		"data",
		&data,
	); err != nil {
		return nil, err
	}
	current, err := currentInvocation(thread)
	if err != nil {
		return nil, err
	}
	var content []byte
	switch data := data.(type) {
	case starlark.String:
		content = []byte(data)
	case starlark.Bytes:
		content = []byte(data)
	default:
		return nil, fmt.Errorf(
			"%s: data must be str or bytes, not %s",
			fn.Name(),
			data.Type(),
		)
	}
	resolved, err := r.resolve(path)
	if err != nil {
		return nil, err
	}
	if err = current.ctx.Err(); err != nil {
		return nil, err
	}
	target, mode, err := writeTarget(resolved)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(
		filepath.Dir(target),
		0o755,
	); err != nil {
		return nil, err
	}
	if err := replaceFile(
		target,
		content,
		mode,
	); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// writeTarget follows symbolic links so that replacing a file keeps its links,
// and reports the permission bits to keep.
func writeTarget(path string) (string, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(
		err,
		fs.ErrNotExist,
	) {
		return path, 0o644, nil
	}
	if err != nil {
		return "", 0, err
	}
	target := path
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", 0, fmt.Errorf(
				"resolve symbolic link: %w",
				err,
			)
		}
		info, err = os.Stat(target)
		if err != nil {
			return "", 0, err
		}
	}
	if !info.Mode().IsRegular() {
		return "", 0, errors.New("path exists and is not a regular file")
	}
	return target, info.Mode().Perm(), nil
}

// replaceFile writes a temporary file beside target and renames it into place,
// so readers never see a partially written file.
func replaceFile(target string, content []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(
		filepath.Dir(target),
		".cpe-write-*",
	)
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	_, writeErr := temporary.Write(content)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(
		writeErr,
		syncErr,
		closeErr,
	); err != nil {
		return err
	}
	if err := os.Chmod(
		temporary.Name(),
		mode,
	); err != nil {
		return err
	}
	return os.Rename(
		temporary.Name(),
		target,
	)
}

func (r *runtime) editFile(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, old, replacement string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"path",
		&path,
		"old",
		&old,
		"new",
		&replacement,
	); err != nil {
		return nil, err
	}
	current, err := currentInvocation(thread)
	if err != nil {
		return nil, err
	}
	resolved, err := r.resolve(path)
	if err != nil {
		return nil, err
	}
	if err := editFile(
		resolved,
		old,
		replacement,
	); err != nil {
		return nil, fmt.Errorf(
			"edit_file %q: %w",
			path,
			err,
		)
	}
	if err := current.ctx.Err(); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// editFile replaces exactly one literal occurrence of old. The unique-match rule
// means a stale or too-short snippet fails instead of editing the wrong place.
func editFile(path, old, replacement string) error {
	if old == "" {
		return errors.New("old text must be nonempty")
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	before, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	text := string(before)
	start := strings.Index(
		text,
		old,
	)
	if start < 0 {
		return errors.New("old text was not found; read the file again and copy the text exactly")
	}
	// Count overlapping occurrences too: "aa" is ambiguous in "aaa".
	matches := 1
	for offset := start + 1; ; matches++ {
		next := strings.Index(
			text[offset:],
			old,
		)
		if next < 0 {
			break
		}
		offset += next + 1
	}
	if matches > 1 {
		return fmt.Errorf(
			"old text matches %d times; include more surrounding text so it matches exactly once",
			matches,
		)
	}
	if old == replacement {
		return errors.New("new text equals old text, so the file would not change")
	}
	after := text[:start] + replacement + text[start+len(old):]
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(
		before,
		current,
	) {
		return errors.New("file changed during the edit; read it again before retrying")
	}
	return replaceFile(
		target,
		[]byte(after),
		info.Mode().Perm(),
	)
}
