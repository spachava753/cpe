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
	managerMetadataRenameFailure
	managerRestoreFailure
	managerStageCleanupFailure
)

type managerFaultFS struct {
	afero.Fs
	fault managerFSFault
}

func (filesystem managerFaultFS) Rename(oldname, newname string) error {
	if filesystem.fault == managerMetadataRenameFailure || filesystem.fault == managerRestoreFailure {
		if filepath.Base(oldname) == "skills.json" || (filesystem.fault == managerRestoreFailure && strings.Contains(
			filepath.ToSlash(oldname),
			"/old/",
		)) {
			return fs.ErrPermission
		}
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
