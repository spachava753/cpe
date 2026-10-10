package bundlefs_test

import (
	"errors"
	"io/fs"

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
