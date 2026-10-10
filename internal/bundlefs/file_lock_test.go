package bundlefs_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestReadFile(t *testing.T) {
	for _, test := range []struct {
		name           string
		content        *string
		readonly       bool
		cancel         bool
		cleanupFailure bool
		wantError      error
	}{
		{
			name: "missing file returns absence",
		},
		{
			name:     "missing read-only lock needs no filesystem writes",
			readonly: true,
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
				if test.content != nil {
					if err := afero.WriteFile(
						filesystem,
						"/lock.json",
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
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				data, err := bundlefs.ReadFile(
					ctx,
					configured,
					"/lock.json",
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
					return
				}
				if test.content == nil {
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
					"/lock.json.cpe-lock",
				); err != nil || exists {
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
