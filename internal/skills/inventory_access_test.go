package skills_test

import (
	"io/fs"

	"github.com/spf13/afero"
)

// Directory search and child access succeed, but opening the inventory fails.
type searchOnlyInventoryFS struct {
	afero.Fs
	root string
}

func (filesystem searchOnlyInventoryFS) Open(name string) (afero.File, error) {
	if name == filesystem.root {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.Open(name)
}

func (filesystem searchOnlyInventoryFS) OpenFile(name string, flags int, mode fs.FileMode) (afero.File, error) {
	if name == filesystem.root {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.OpenFile(
		name,
		flags,
		mode,
	)
}

func (filesystem searchOnlyInventoryFS) LstatIfPossible(name string) (fs.FileInfo, bool, error) {
	if underlying, ok := filesystem.Fs.(afero.Lstater); ok {
		return underlying.LstatIfPossible(name)
	}
	info, err := filesystem.Stat(name)
	return info, false, err
}
