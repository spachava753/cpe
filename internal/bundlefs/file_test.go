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

func TestApplyWithFile(t *testing.T) {
	for _, test := range []struct {
		name           string
		native         bool
		absent         bool
		unchanged      bool
		stageFailure   bool
		publishFailure bool
		bundleFault    transactionFault
		wantBundle     string
		wantTracking   string
		wantOutcome    bundlefs.Outcome
		wantError      error
	}{
		{
			name:         "replace tracking after publishing bundles",
			wantBundle:   "new bundle",
			wantTracking: "new tracking",
			wantOutcome:  bundlefs.Committed,
		},
		{
			name:         "native filesystem replaces existing tracking",
			native:       true,
			wantBundle:   "new bundle",
			wantTracking: "new tracking",
			wantOutcome:  bundlefs.Committed,
		},
		{
			name:         "create missing tracking",
			absent:       true,
			wantBundle:   "new bundle",
			wantTracking: "new tracking",
			wantOutcome:  bundlefs.Committed,
		},
		{
			name:         "nil data keeps tracking unchanged",
			unchanged:    true,
			wantBundle:   "new bundle",
			wantTracking: "old tracking",
			wantOutcome:  bundlefs.Committed,
		},
		{
			name:         "tracking staging failure leaves bundles untouched",
			stageFailure: true,
			wantBundle:   "old bundle",
			wantTracking: "old tracking",
			wantError:    fs.ErrPermission,
		},
		{
			name:           "tracking rename failure rolls back bundles",
			publishFailure: true,
			wantBundle:     "old bundle",
			wantTracking:   "old tracking",
			wantError:      fs.ErrPermission,
		},
		{
			name:         "bundle failure leaves tracking untouched",
			bundleFault:  backupFault,
			wantBundle:   "old bundle",
			wantTracking: "old tracking",
			wantError:    fs.ErrPermission,
		},
		{
			name:         "cancellation before tracking publication rolls back bundles",
			bundleFault:  cancelPublication,
			wantBundle:   "old bundle",
			wantTracking: "old tracking",
			wantError:    context.Canceled,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				if test.native {
					filesystem = afero.NewBasePathFs(
						afero.NewOsFs(),
						t.TempDir(),
					)
				}
				root := filepath.FromSlash("skills")
				tracking := filepath.FromSlash("skills-lock.json")
				bundle := filepath.Join(
					root,
					"beta",
					"marker",
				)
				if err := filesystem.MkdirAll(
					filepath.Dir(bundle),
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := afero.WriteFile(
					filesystem,
					bundle,
					[]byte("old bundle"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if !test.absent {
					if err := afero.WriteFile(
						filesystem,
						tracking,
						[]byte("old tracking"),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				data := []byte("new tracking")
				if test.unchanged {
					data = nil
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				outcome, err := bundlefs.ApplyWithFile(
					ctx,
					fileFaultFS{
						Fs: transactionFS{
							Fs:     filesystem,
							fault:  test.bundleFault,
							cancel: cancel,
						},
						tracking:       tracking,
						stageFailure:   test.stageFailure,
						publishFailure: test.publishFailure,
					},
					root,
					[]bundlefs.Change{
						{
							Name:   "beta",
							Action: bundlefs.Replace,
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
						Path: tracking,
						Data: data,
					},
				)
				if outcome != test.wantOutcome || !errors.Is(
					err,
					test.wantError,
				) {
					t.Fatalf(
						"outcome = %v, error = %v; want %v, %v",
						outcome,
						err,
						test.wantOutcome,
						test.wantError,
					)
				}
				for filename, want := range map[string]string{
					bundle:   test.wantBundle,
					tracking: test.wantTracking,
				} {
					data, err := afero.ReadFile(
						filesystem,
						filename,
					)
					if err != nil || string(data) != want {
						t.Fatalf(
							"%s = %q, %v; want %q",
							filename,
							data,
							err,
							want,
						)
					}
				}
				for _, directory := range []string{
					".",
					root,
				} {
					entries, err := afero.ReadDir(
						filesystem,
						directory,
					)
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						if strings.HasPrefix(
							entry.Name(),
							".cpe-",
						) {
							t.Fatalf(
								"unexpected staging file: %s",
								entry.Name(),
							)
						}
					}
				}
			},
		)
	}
}

type fileFaultFS struct {
	afero.Fs
	tracking       string
	stageFailure   bool
	publishFailure bool
}

func (filesystem fileFaultFS) OpenFile(filename string, flags int, mode fs.FileMode) (afero.File, error) {
	if filesystem.stageFailure && strings.HasPrefix(
		filepath.Base(filename),
		".cpe-file-",
	) {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.OpenFile(
		filename,
		flags,
		mode,
	)
}

func (filesystem fileFaultFS) Rename(oldName, newName string) error {
	if filesystem.publishFailure && newName == filesystem.tracking {
		return fs.ErrPermission
	}
	return filesystem.Fs.Rename(
		oldName,
		newName,
	)
}
