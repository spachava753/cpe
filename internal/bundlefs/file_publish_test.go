package bundlefs_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestPublicationDoesNotReplaceAppearingFile(t *testing.T) {
	for _, test := range []struct {
		name     string
		native   bool
		existing bool
	}{
		{
			name: "new metadata file in memory",
		},
		{
			name:     "replacement metadata file in memory",
			existing: true,
		},
		{
			name:   "new metadata file on native filesystem",
			native: true,
		},
		{
			name:     "replacement metadata file on native filesystem",
			native:   true,
			existing: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				base := t.TempDir()
				root := filepath.Join(
					base,
					".agents",
					"skills",
				)
				filename := filepath.Join(
					base,
					"skills-lock.json",
				)
				marker := filepath.Join(
					root,
					"alpha",
					"marker",
				)
				filesystem := afero.NewMemMapFs()
				if test.native {
					filesystem = afero.NewOsFs()
				}
				if err := filesystem.MkdirAll(
					filepath.Dir(marker),
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				if err := afero.WriteFile(
					filesystem,
					marker,
					[]byte("old bundle"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				var previous []byte
				if test.existing {
					previous = []byte("old lock")
					if err := afero.WriteFile(
						filesystem,
						filename,
						previous,
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				outcome, err := bundlefs.ApplyWithFile(
					t.Context(),
					&appearingFileFS{
						Fs:       filesystem,
						filename: filename,
					},
					root,
					[]bundlefs.Change{
						{
							Name:   "alpha",
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
						Path:     filename,
						Data:     []byte("new lock"),
						Previous: previous,
					},
				)
				if outcome != bundlefs.NotCommitted || !errors.Is(
					err,
					fs.ErrExist,
				) {
					t.Fatalf(
						"ApplyWithFile = %v, %v; want conflicting publication",
						outcome,
						err,
					)
				}
				for name, want := range map[string]string{
					marker:   "old bundle",
					filename: "external lock",
				} {
					data, err := afero.ReadFile(
						filesystem,
						name,
					)
					if err != nil || string(data) != want {
						t.Fatalf(
							"%s = %q, %v; want %q",
							name,
							data,
							err,
							want,
						)
					}
				}
				if test.existing && !strings.Contains(
					err.Error(),
					"recovery file retained",
				) {
					t.Fatal("unreplaced original lock backup was not identified")
				}
			},
		)
	}
}

type appearingFileFS struct {
	afero.Fs
	filename string
	once     sync.Once
	err      error
}

func (filesystem *appearingFileFS) appear() error {
	filesystem.once.Do(func() {
		filesystem.err = afero.WriteFile(
			filesystem.Fs,
			filesystem.filename,
			[]byte("external lock"),
			0o600,
		)
	})
	return filesystem.err
}

func (filesystem *appearingFileFS) Open(filename string) (afero.File, error) {
	if filename == filepath.Dir(filesystem.filename) {
		if err := filesystem.appear(); err != nil {
			return nil, err
		}
	}
	return filesystem.Fs.Open(filename)
}

func (filesystem *appearingFileFS) Rename(oldname string, newname string) error {
	if newname == filesystem.filename && strings.HasPrefix(
		filepath.Base(oldname),
		".cpe-file-",
	) {
		if err := filesystem.appear(); err != nil {
			return err
		}
	}
	return filesystem.Fs.Rename(
		oldname,
		newname,
	)
}
