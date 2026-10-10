package bundlefs_test

import (
	"errors"
	"io/fs"
	"os"

	"github.com/spf13/afero"
)

// Reproduce a reader's missing-file stat straddling a completed publication.
// Return the old stat result after making the new bytes available.
type finishingPublicationFS struct {
	afero.Fs
	filename string
	finished bool
}

func (filesystem *finishingPublicationFS) LstatIfPossible(filename string) (fs.FileInfo, bool, error) {
	info, err := filesystem.Stat(filename)
	if filename == filesystem.filename && errors.Is(
		err,
		fs.ErrNotExist,
	) && !filesystem.finished {
		filesystem.finished = true
		if err := afero.WriteFile(
			filesystem.Fs,
			filename,
			[]byte("new lock"),
			0o600,
		); err != nil {
			return nil, false, err
		}
	}
	return info, false, err
}

// Complete the backing writer after a read-only reader cannot create a guard.
// The reader must report acquisition failure, not an untracked snapshot.
type readonlyFinishingFS struct {
	afero.Fs
	writable afero.Fs
	filename string
}

func (filesystem *readonlyFinishingFS) OpenFile(filename string, flags int, mode fs.FileMode) (afero.File, error) {
	if filename == filesystem.filename+".cpe-lock" && flags&os.O_CREATE != 0 {
		if err := afero.WriteFile(
			filesystem.writable,
			filesystem.filename,
			[]byte("new lock"),
			0o600,
		); err != nil {
			return nil, err
		}
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.OpenFile(
		filename,
		flags,
		mode,
	)
}
