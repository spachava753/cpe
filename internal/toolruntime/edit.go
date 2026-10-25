package toolruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spf13/afero"
)

func (h *toolHost) edit(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var filename, old, replacement string
	if err := starlark.UnpackArgs(
		fn.Name(),
		args,
		kwargs,
		"filename",
		&filename,
		"old",
		&old,
		"new",
		&replacement,
	); err != nil {
		return nil, err
	}
	if filename == "" || old == "" {
		return nil, errors.New("edit requires a filename and nonempty old text")
	}
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(
			h.config.Cwd,
			filename,
		)
	}
	ctx := dyson.EvaluationContext(thread)
	current, ok := ctx.Value(invocationKey{}).(*invocation)
	if !ok {
		return nil, errors.New("edit outside an invocation")
	}
	diff, err := editFile(
		ctx,
		h.stdlib.FS,
		filename,
		old,
		replacement,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"edit %q: %w",
			filename,
			err,
		)
	}
	current.output.add(gai.TextBlock(fmt.Sprintf(
		"Edited %q: replaced one occurrence.\n%s",
		filename,
		diff,
	)))
	return starlark.None, nil
}

func editFile(ctx context.Context, filesystem afero.Fs, filename, old, replacement string) (string, error) {
	if filesystem == nil {
		return "", errors.New("filesystem is not configured")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	target, editErr := editTarget(
		filesystem,
		filename,
	)
	if editErr != nil {
		return "", editErr
	}
	info, editErr := filesystem.Stat(target)
	if editErr != nil {
		return "", editErr
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("edit requires a regular file")
	}
	before, editErr := afero.ReadFile(
		filesystem,
		target,
	)
	if editErr != nil {
		return "", editErr
	}
	text := string(before)
	start := strings.Index(
		text,
		old,
	)
	if start < 0 {
		return "", errors.New("old text was not found")
	}
	// Include overlapping occurrences: "aa" is ambiguous in "aaa".
	if strings.Contains(
		text[start+1:],
		old,
	) {
		return "", errors.New("old text is ambiguous; provide more surrounding text")
	}
	if old == replacement {
		return "", errors.New("replacement would not change the file")
	}
	after := text[:start] + replacement + text[start+len(old):]
	temporary, editErr := afero.TempFile(
		filesystem,
		filepath.Dir(target),
		".cpe-edit-",
	)
	if editErr != nil {
		return "", editErr
	}
	defer filesystem.Remove(temporary.Name())
	written, writeErr := temporary.WriteString(after)
	if writeErr == nil && written != len(after) {
		writeErr = io.ErrShortWrite
	}
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(
		writeErr,
		syncErr,
		closeErr,
	); err != nil {
		return "", err
	}
	if err := filesystem.Chmod(
		temporary.Name(),
		info.Mode().Perm(),
	); err != nil {
		return "", err
	}
	current, editErr := afero.ReadFile(
		filesystem,
		target,
	)
	if editErr != nil {
		return "", editErr
	}
	if !bytes.Equal(
		before,
		current,
	) {
		return "", errors.New("file changed during edit; read it again before retrying")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := filesystem.Rename(
		temporary.Name(),
		target,
	); err != nil {
		return "", err
	}
	return editDiff(
		filename,
		text,
		start,
		old,
		replacement,
	), nil
}

// Resolve links through the injected filesystem so replacement preserves the links.
func editTarget(filesystem afero.Fs, filename string) (string, error) {
	volume := filepath.VolumeName(filename)
	resolved := volume + string(filepath.Separator)
	pending := strings.Split(
		strings.TrimPrefix(
			filename,
			resolved,
		),
		string(filepath.Separator),
	)
	links := 0
	for len(pending) != 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		candidate := filepath.Join(
			resolved,
			part,
		)
		var info fs.FileInfo
		var err error
		if filesystem, ok := filesystem.(afero.Lstater); ok {
			info, _, err = filesystem.LstatIfPossible(candidate)
		} else {
			return "", errors.New("filesystem does not support inspecting file links")
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			resolved = candidate
			continue
		}
		links++
		if links > 40 {
			return "", errors.New("too many symbolic links")
		}
		reader, ok := filesystem.(afero.LinkReader)
		if !ok {
			return "", errors.New("filesystem does not support reading symbolic links")
		}
		target, err := reader.ReadlinkIfPossible(candidate)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) {
			resolved = filepath.VolumeName(target) + string(filepath.Separator)
			target = strings.TrimPrefix(
				target,
				resolved,
			)
		}
		pending = append(
			strings.Split(
				target,
				string(filepath.Separator),
			),
			pending...,
		)
	}
	return resolved, nil
}

// A literal replacement needs one hunk covering the affected whole lines.
func editDiff(filename, before string, start int, old, replacement string) string {
	first := strings.LastIndexByte(
		before[:start],
		'\n',
	) + 1
	last := start + len(old)
	if newline := strings.IndexByte(
		before[last:],
		'\n',
	); newline >= 0 {
		last += newline + 1
	} else {
		last = len(before)
	}
	removed := before[first:last]
	added := before[first:start] + replacement + before[start+len(old):last]
	line := strings.Count(
		before[:first],
		"\n",
	) + 1
	oldLines := editLineCount(removed)
	newLines := editLineCount(added)
	newLine := line
	if newLines == 0 {
		newLine--
	}
	var diff strings.Builder
	// Quote paths containing whitespace or control characters in the diff headers.
	if strings.ContainsAny(
		filename,
		" \t\r\n\"\\",
	) {
		filename = strconv.Quote(filename)
	}
	fmt.Fprintf(
		&diff,
		"--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n",
		filename,
		filename,
		line,
		oldLines,
		newLine,
		newLines,
	)
	for i, text := range []string{
		removed,
		added,
	} {
		for line := range strings.SplitAfterSeq(
			text,
			"\n",
		) {
			if line == "" {
				continue
			}
			diff.WriteByte("-+"[i])
			diff.WriteString(line)
			if !strings.HasSuffix(
				line,
				"\n",
			) {
				diff.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return diff.String()
}

func editLineCount(text string) int {
	count := strings.Count(
		text,
		"\n",
	)
	if text != "" && !strings.HasSuffix(
		text,
		"\n",
	) {
		count++
	}
	return count
}
