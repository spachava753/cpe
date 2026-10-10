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

func TestReadFile(t *testing.T) {
	for _, test := range []struct {
		name                string
		content             *string
		readonly            bool
		nativeSidecar       bool
		cancel              bool
		cleanupFailure      bool
		finishPublication   bool
		readonlyPublication bool
		wantError           error
	}{
		{
			name: "missing file returns absence",
		},
		{
			name:      "read-only absence without a guard safely aborts",
			readonly:  true,
			wantError: fs.ErrPermission,
		},
		{
			name:                "read-only gap detection aborts even when publication completes",
			readonlyPublication: true,
			wantError:           fs.ErrPermission,
		},
		{
			name:          "retained native sidecar supports read-only absent lock",
			readonly:      true,
			nativeSidecar: true,
		},
		{
			name:          "retained native sidecar supports read-only regular lock",
			readonly:      true,
			nativeSidecar: true,
			content:       new("lock bytes"),
		},
		{
			name:    "empty file is distinct from absence",
			content: new(""),
		},
		{
			name:    "regular bytes are preserved",
			content: new("lock bytes"),
		},
		{
			name:              "publication completing after a missing stat is observed",
			finishPublication: true,
		},
		{
			name:      "canceled reads do not acquire guards",
			cancel:    true,
			wantError: context.Canceled,
		},
		{
			name:           "guard cleanup errors are reported",
			content:        new("lock bytes"),
			cleanupFailure: true,
			wantError:      fs.ErrPermission,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				root := t.TempDir()
				filename := filepath.Join(
					root,
					"lock.json",
				)
				if test.nativeSidecar {
					filesystem = afero.NewOsFs()
					if err := afero.WriteFile(
						filesystem,
						filename+".cpe-lock",
						nil,
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				} else if err := filesystem.MkdirAll(
					root,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if test.content != nil {
					if err := afero.WriteFile(
						filesystem,
						filename,
						[]byte(*test.content),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				configured := filesystem
				if test.readonly {
					configured = afero.NewReadOnlyFs(filesystem)
				}
				if test.cleanupFailure {
					configured = guardCleanupFailureFS{
						Fs: filesystem,
					}
				}
				if test.finishPublication {
					configured = &finishingPublicationFS{
						Fs:       filesystem,
						filename: filename,
					}
				}
				if test.readonlyPublication {
					configured = &readonlyFinishingFS{
						Fs:       afero.NewReadOnlyFs(filesystem),
						writable: filesystem,
						filename: filename,
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				data, err := bundlefs.ReadFile(
					ctx,
					configured,
					filename,
				)
				if !errors.Is(
					err,
					test.wantError,
				) {
					t.Fatalf(
						"ReadFile = %q, %v; want error %v",
						data,
						err,
						test.wantError,
					)
				}
				if test.wantError != nil {
					if test.readonlyPublication {
						committed, readErr := afero.ReadFile(
							filesystem,
							filename,
						)
						if readErr != nil || string(committed) != "new lock" {
							t.Fatalf(
								"publication did not complete during the read: %q, %v",
								committed,
								readErr,
							)
						}
					}
					return
				}
				if test.finishPublication {
					if string(data) != "new lock" {
						t.Fatalf(
							"completed publication was mistaken for absence: %q",
							data,
						)
					}
				} else if test.content == nil {
					if data != nil {
						t.Fatal("missing lock did not return absence")
					}
				} else if data == nil || string(data) != *test.content {
					t.Fatalf(
						"ReadFile = %q; want %q",
						data,
						*test.content,
					)
				}
				if exists, err := afero.Exists(
					filesystem,
					filename+".cpe-lock",
				); err != nil || exists != test.nativeSidecar {
					t.Fatalf(
						"memory guard was not released: %v, %v",
						exists,
						err,
					)
				}
			},
		)
	}
}

type guardCleanupFailureFS struct {
	afero.Fs
}

func (filesystem guardCleanupFailureFS) Remove(filename string) error {
	if strings.HasSuffix(
		filename,
		".cpe-lock",
	) {
		return fs.ErrPermission
	}
	return filesystem.Fs.Remove(filename)
}
