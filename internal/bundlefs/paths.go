package bundlefs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/afero"
)

// Lstat returns metadata for filename using afero.Lstater when supported.
// Otherwise it falls back to Stat, which may follow the final symlink.
func Lstat(filesystem afero.Fs, filename string) (fs.FileInfo, error) {
	if filesystem, ok := filesystem.(afero.Lstater); ok {
		info, _, err := filesystem.LstatIfPossible(filename)
		return info, err
	}
	return filesystem.Stat(filename)
}

// CheckDestination checks a native installation path for symlink and non-directory
// components using Lstat. Missing components are allowed; link/.. is still checked.
// Empty paths, parent traversal after missing components, Windows drive-relative
// paths, nil filesystems, and canceled contexts return an error.
func CheckDestination(ctx context.Context, filesystem afero.Fs, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if filesystem == nil {
		return errors.New("destination filesystem is unavailable")
	}
	if strings.TrimSpace(directory) == "" {
		return errors.New("destination directory is required")
	}
	return rejectDestinationLinks(
		filesystem,
		directory,
	)
}

// Inspect native components before cleaning them, so link/.. cannot hide a link.
func rejectDestinationLinks(filesystem afero.Fs, directory string) error {
	// Windows accepts both separators. Normalize them without cleaning away
	// link/.., which must still inspect the link before parent traversal.
	directory = filepath.FromSlash(directory)
	volume := filepath.VolumeName(directory)
	if volume != "" && !filepath.IsAbs(directory) {
		return errors.New("destination drive-relative paths are not supported")
	}
	prefix := volume
	remaining := directory[len(volume):]
	if filepath.IsAbs(directory) || strings.HasPrefix(
		remaining,
		string(filepath.Separator),
	) {
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
		info, err := Lstat(
			filesystem,
			prefix,
		)
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

// ValidDirectoryName reports whether name is a nonempty UTF-8 directory name.
// It rejects ".", "..", slash, backslash, colon, and NUL bytes.
func ValidDirectoryName(name string) bool {
	return name != "" && name != "." && name != ".." && utf8.ValidString(name) && !strings.ContainsAny(
		name,
		"/\\\x00:",
	) && filepath.Base(name) == name
}

// CheckSourceDirectory checks that directory is an existing, symlink-free source
// directory using io/fs-relative slash paths. "." selects the established root.
// Invalid paths, missing components, and non-directories return an error.
func CheckSourceDirectory(filesystem fs.FS, directory string) error {
	if !fs.ValidPath(directory) || strings.ContainsAny(
		directory,
		"\\\x00",
	) {
		return fmt.Errorf(
			"invalid source directory %q",
			directory,
		)
	}
	if directory == "." {
		return nil
	}
	parent := "."
	for component := range strings.SplitSeq(
		directory,
		"/",
	) {
		entries, err := fs.ReadDir(
			filesystem,
			parent,
		)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			if entry.Name() != component {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf(
					"source directory %s is a symlink, not a regular directory",
					path.Join(
						parent,
						component,
					),
				)
			}
			if !info.IsDir() {
				return fmt.Errorf(
					"source path %s must be a regular directory",
					path.Join(
						parent,
						component,
					),
				)
			}
			found = true
			break
		}
		if !found {
			return &fs.PathError{
				Op:   "read source directory",
				Path: directory,
				Err:  fs.ErrNotExist,
			}
		}
		parent = path.Join(
			parent,
			component,
		)
	}
	return nil
}
