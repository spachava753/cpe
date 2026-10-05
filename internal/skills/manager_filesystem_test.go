package skills_test

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// Failure and link-capable adapters keep actual file contents on MemMapFs.
type managerFSFault uint8

const (
	managerNoFault managerFSFault = iota
	managerPublishFailure
	managerLatePublishFailure
	managerRestoreFailure
	managerStageCleanupFailure
)

type managerFaultFS struct {
	afero.Fs
	fault managerFSFault
}

func (filesystem managerFaultFS) Rename(oldname, newname string) error {
	failPublish := filesystem.fault == managerPublishFailure ||
		filesystem.fault == managerRestoreFailure ||
		(filesystem.fault == managerLatePublishFailure && filepath.Base(oldname) == "beta")
	if (failPublish && strings.Contains(
		filepath.ToSlash(oldname),
		"/new/",
	)) || (filesystem.fault == managerRestoreFailure && strings.Contains(
		filepath.ToSlash(oldname),
		"/old/",
	)) {
		return fs.ErrPermission
	}
	return filesystem.Fs.Rename(
		oldname,
		newname,
	)
}
func (filesystem managerFaultFS) RemoveAll(name string) error {
	if filesystem.fault == managerStageCleanupFailure && strings.HasPrefix(
		filepath.Base(name),
		".cpe-stage-",
	) {
		return fs.ErrPermission
	}
	return filesystem.Fs.RemoveAll(name)
}

type managerLinkFS struct {
	afero.Fs
	links map[string]bool
}
type managerLinkInfo struct{ fs.FileInfo }

type managerLinkFile struct {
	afero.File
	links map[string]bool
}

func (file managerLinkFile) Readdir(count int) ([]fs.FileInfo, error) {
	entries, err := file.File.Readdir(count)
	for i, entry := range entries {
		if file.links[filepath.Clean(filepath.Join(
			file.Name(),
			entry.Name(),
		))] {
			entries[i] = managerLinkInfo{
				entry,
			}
		}
	}
	return entries, err
}

func (filesystem managerLinkFS) Open(name string) (afero.File, error) {
	file, err := filesystem.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return managerLinkFile{
		File:  file,
		links: filesystem.links,
	}, nil
}

func (info managerLinkInfo) Mode() fs.FileMode { return fs.ModeSymlink | 0o777 }
func (info managerLinkInfo) IsDir() bool       { return false }
func (filesystem managerLinkFS) LstatIfPossible(name string) (fs.FileInfo, bool, error) {
	info, err := filesystem.Stat(name)
	if err != nil {
		return nil, true, err
	}
	if filesystem.links[filepath.Clean(name)] {
		return managerLinkInfo{
			info,
		}, true, nil
	}
	return info, true, nil
}
