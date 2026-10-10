package bundlefs_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestRestoredFileCleanupError(t *testing.T) {
	filesystem := afero.NewMemMapFs()
	const root = "/skills"
	const filename = "/state/lock.json"
	if err := filesystem.MkdirAll(
		"/state",
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(
		filesystem,
		filename,
		[]byte("old lock"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &restoreCleanupFS{
		Fs:       filesystem,
		filename: filename,
		cancel:   cancel,
	}
	outcome, err := bundlefs.ApplyWithFile(
		ctx,
		probe,
		root,
		[]bundlefs.Change{
			{
				Name:   "alpha",
				Action: bundlefs.Install,
				Payload: []bundlefs.Entry{
					{
						Path: "marker",
						Data: []byte("new bundle"),
					},
				},
			},
		},
		func(string) error { return nil },
		bundlefs.FileChange{
			Path:     filename,
			Data:     []byte("new lock"),
			Previous: []byte("old lock"),
		},
	)
	if outcome != bundlefs.NotCommitted || !errors.Is(
		err,
		fs.ErrPermission,
	) || !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf(
			"ApplyWithFile = %v, %v",
			outcome,
			err,
		)
	}
	data, readErr := afero.ReadFile(
		filesystem,
		filename,
	)
	if readErr != nil || string(data) != "old lock" {
		t.Fatalf(
			"restored lock = %q, %v",
			data,
			readErr,
		)
	}
	if !strings.Contains(
		err.Error(),
		"file restored at "+filename,
	) || strings.Contains(
		err.Error(),
		"recovery file retained at "+probe.source,
	) {
		t.Fatalf(
			"consumed restoration source was reported as recovery: %v",
			err,
		)
	}
	if exists, statErr := afero.Exists(
		filesystem,
		probe.source,
	); statErr != nil || exists {
		t.Fatalf(
			"source-consuming restoration was not modeled: %v, %v",
			exists,
			statErr,
		)
	}
}

// Model Windows' source-consuming publication followed by a directory-close
// failure. This exercises the result contract without claiming a Windows run.
type restoreCleanupFS struct {
	afero.Fs
	filename  string
	cancel    context.CancelFunc
	source    string
	published bool
}

func (filesystem *restoreCleanupFS) Open(filename string) (afero.File, error) {
	file, err := filesystem.Fs.Open(filename)
	if err != nil {
		return nil, err
	}
	if filesystem.published && strings.HasPrefix(
		filepath.Base(filename),
		".cpe-backup-",
	) {
		filesystem.source = filename
	}
	if filename == filepath.Dir(filesystem.filename) {
		return wrapClosingFile(
			file,
			func() error {
				if filesystem.source != "" {
					return errors.Join(
						filesystem.Remove(filesystem.source),
						fs.ErrPermission,
					)
				}
				filesystem.published = true
				filesystem.cancel()
				return nil
			},
		), nil
	}
	return file, nil
}
