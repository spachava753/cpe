package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func validDirectoryName(name string) bool {
	return name != "" && name != "." && name != ".." && utf8.ValidString(name) && !strings.ContainsAny(
		name,
		"/\\\x00:",
	) && filepath.Base(name) == name
}

func (m *Manager) destination(ctx context.Context, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m == nil || m.destinations == nil {
		return errors.New("destination filesystem is unavailable")
	}
	if strings.TrimSpace(directory) == "" {
		return errors.New("destination directory is required")
	}
	return m.rejectDestinationLinks(directory)
}

// Inspect native components before cleaning them, so link/.. cannot hide a link.
func (m *Manager) rejectDestinationLinks(directory string) error {
	// Windows accepts both separators. Normalize them without cleaning away
	// link/.., which must still inspect the link before parent traversal.
	directory = filepath.FromSlash(directory)
	volume := filepath.VolumeName(directory)
	prefix := volume
	remaining := directory[len(volume):]
	if filepath.IsAbs(directory) {
		prefix += string(filepath.Separator)
	}
	unresolved := false
	for component := range strings.SplitSeq(
		remaining,
		string(filepath.Separator),
	) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." && unresolved {
			return errors.New("destination parent traversal follows a missing directory component")
		}
		if prefix != "" && !strings.HasSuffix(
			prefix,
			string(filepath.Separator),
		) {
			prefix += string(filepath.Separator)
		}
		prefix += component
		info, err := m.pathInfo(prefix)
		if errors.Is(
			err,
			fs.ErrNotExist,
		) {
			unresolved = true
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf(
				"destination path %s is a symlink",
				prefix,
			)
		}
		if !info.IsDir() {
			return fmt.Errorf(
				"destination path %s is not a directory",
				prefix,
			)
		}
	}
	return nil
}

func selectedNames(names []string) ([]string, error) {
	seen := map[string]bool{}
	result := make(
		[]string,
		0,
		len(names),
	)
	for _, name := range names {
		if !validDirectoryName(name) {
			return nil, fmt.Errorf(
				"invalid plugin directory name %q",
				name,
			)
		}
		if !seen[name] {
			seen[name] = true
			result = append(
				result,
				name,
			)
		}
	}
	return result, nil
}
