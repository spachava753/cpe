package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/afero/mem"
)

// installedFilesystem resolves components through opened, non-following parents.
// Its root remains owned until installed-content verification finishes.
type installedFilesystem struct {
	filesystem afero.Fs
	root       afero.File
}

func openInstalledFilesystem(ctx context.Context, filesystem afero.Fs, directory string) (*installedFilesystem, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("installed directory must be absolute")
	}
	rootPath := filepath.VolumeName(directory) + string(filepath.Separator)
	parent, err := filesystem.OpenFile(
		rootPath,
		installedParentFlags,
		0,
	)
	if err != nil {
		return nil, err
	}
	if err := checkInstalledHandle(
		parent,
		true,
	); err != nil {
		return nil, errors.Join(
			err,
			parent.Close(),
		)
	}
	relative, err := filepath.Rel(
		rootPath,
		filepath.Clean(directory),
	)
	if err != nil {
		return nil, errors.Join(
			err,
			parent.Close(),
		)
	}
	if relative != "." {
		for name := range strings.SplitSeq(
			filepath.ToSlash(relative),
			"/",
		) {
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(
					err,
					parent.Close(),
				)
			}
			child, err := openInstalledChild(
				filesystem,
				parent,
				name,
				installedParentFlags,
			)
			closeErr := parent.Close()
			if err != nil {
				return nil, errors.Join(
					err,
					closeErr,
				)
			}
			if err := errors.Join(
				checkInstalledHandle(
					child,
					true,
				),
				closeErr,
			); err != nil {
				return nil, errors.Join(
					err,
					child.Close(),
				)
			}
			parent = child
		}
	}
	return &installedFilesystem{
		filesystem: filesystem,
		root:       parent,
	}, nil
}

func (filesystem *installedFilesystem) Close() error {
	return filesystem.root.Close()
}

func (filesystem *installedFilesystem) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	components := strings.Split(
		name,
		"/",
	)
	parent := filesystem.root
	for index, component := range components {
		flags := installedOpenFlags
		directory := index < len(components)-1
		if directory {
			flags = installedParentFlags
		}
		child, err := openInstalledChild(
			filesystem.filesystem,
			parent,
			component,
			flags,
		)
		var closeErr error
		if index > 0 {
			closeErr = parent.Close()
		}
		if err != nil {
			return nil, errors.Join(
				err,
				closeErr,
			)
		}
		if err := errors.Join(
			checkInstalledHandle(
				child,
				directory,
			),
			closeErr,
		); err != nil {
			return nil, errors.Join(
				err,
				child.Close(),
			)
		}
		parent = child
	}
	return installedFile{
		File: parent,
	}, nil
}

// Relative acquisition can be supplied by filesystem adapters. Native files use
// directory handles; memory files have no symlinks and retain injected operations.
func openInstalledChild(filesystem afero.Fs, parent afero.File, name string, flags int) (afero.File, error) {
	if relative, ok := filesystem.(interface {
		OpenFileAt(afero.File, string, int, fs.FileMode) (afero.File, error)
	}); ok {
		return relative.OpenFileAt(
			parent,
			name,
			flags,
			0,
		)
	}
	filename := filepath.Join(
		parent.Name(),
		name,
	)
	if descriptor, native := installedDescriptor(parent); native {
		return openInstalledNative(
			descriptor,
			name,
			filename,
			flags,
		)
	}
	info, err := parent.Stat()
	if err != nil {
		return nil, err
	}
	if _, memory := info.(*mem.FileInfo); !memory {
		return nil, errors.New("filesystem does not expose directory-relative installed reads")
	}
	return filesystem.OpenFile(
		filename,
		flags,
		0,
	)
}

func installedDescriptor(file afero.File) (uintptr, bool) {
	if native, ok := file.(interface{ Fd() uintptr }); ok {
		return native.Fd(), true
	}
	if bounded, ok := file.(*afero.BasePathFile); ok {
		return installedDescriptor(bounded.File)
	}
	return 0, false
}

func checkInstalledHandle(file afero.File, directory bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || (directory && !info.IsDir()) || (!info.IsDir() && !info.Mode().IsRegular()) {
		return fmt.Errorf(
			"installed payload %s is not a regular %s",
			file.Name(),
			installedKind(directory),
		)
	}
	return nil
}

func installedKind(directory bool) string {
	if directory {
		return "directory"
	}
	return "file or directory"
}

type installedFile struct {
	afero.File
}

func (file installedFile) ReadDir(count int) ([]fs.DirEntry, error) {
	infos, err := file.Readdir(count)
	entries := make(
		[]fs.DirEntry,
		0,
		len(infos),
	)
	for _, info := range infos {
		entries = append(
			entries,
			fs.FileInfoToDirEntry(info),
		)
	}
	return entries, err
}
