package skills_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// Model alternate root spellings backed by one directory, without relying on a
// case-insensitive host filesystem. Child paths and names remain unchanged.
type rootAliasFS struct {
	afero.Fs
	alias string
	root  string
}

func (filesystem rootAliasFS) path(name string) string {
	if name == filesystem.alias || strings.HasPrefix(
		name,
		filesystem.alias+string(filepath.Separator),
	) {
		return filesystem.root + name[len(filesystem.alias):]
	}
	return name
}

func (filesystem rootAliasFS) Open(name string) (afero.File, error) {
	return filesystem.Fs.Open(filesystem.path(name))
}
func (filesystem rootAliasFS) OpenFile(name string, flags int, mode fs.FileMode) (afero.File, error) {
	return filesystem.Fs.OpenFile(
		filesystem.path(name),
		flags,
		mode,
	)
}
func (filesystem rootAliasFS) Create(name string) (afero.File, error) {
	return filesystem.Fs.Create(filesystem.path(name))
}
func (filesystem rootAliasFS) Stat(name string) (fs.FileInfo, error) {
	return filesystem.Fs.Stat(filesystem.path(name))
}
func (filesystem rootAliasFS) LstatIfPossible(name string) (fs.FileInfo, bool, error) {
	if underlying, ok := filesystem.Fs.(afero.Lstater); ok {
		return underlying.LstatIfPossible(filesystem.path(name))
	}
	info, err := filesystem.Stat(name)
	return info, false, err
}
func (filesystem rootAliasFS) Mkdir(name string, mode fs.FileMode) error {
	return filesystem.Fs.Mkdir(
		filesystem.path(name),
		mode,
	)
}
func (filesystem rootAliasFS) MkdirAll(name string, mode fs.FileMode) error {
	return filesystem.Fs.MkdirAll(
		filesystem.path(name),
		mode,
	)
}
func (filesystem rootAliasFS) Remove(name string) error {
	return filesystem.Fs.Remove(filesystem.path(name))
}
func (filesystem rootAliasFS) RemoveAll(name string) error {
	return filesystem.Fs.RemoveAll(filesystem.path(name))
}
func (filesystem rootAliasFS) Rename(oldname string, newname string) error {
	return filesystem.Fs.Rename(
		filesystem.path(oldname),
		filesystem.path(newname),
	)
}
func (filesystem rootAliasFS) Chmod(name string, mode fs.FileMode) error {
	return filesystem.Fs.Chmod(
		filesystem.path(name),
		mode,
	)
}
func (filesystem rootAliasFS) Chown(name string, uid int, gid int) error {
	return filesystem.Fs.Chown(
		filesystem.path(name),
		uid,
		gid,
	)
}
func (filesystem rootAliasFS) Chtimes(name string, access time.Time, modification time.Time) error {
	return filesystem.Fs.Chtimes(
		filesystem.path(name),
		access,
		modification,
	)
}
