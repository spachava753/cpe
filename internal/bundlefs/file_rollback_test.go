package bundlefs_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestRollbackPreservesLateReplacement(t *testing.T) {
	for _, native := range []bool{
		false,
		true,
	} {
		for _, existing := range []bool{
			false,
			true,
		} {
			for _, point := range []string{
				"after publication",
				"during withdrawal",
			} {
				t.Run(
					fmt.Sprintf(
						"native=%v/existing=%v/%s",
						native,
						existing,
						point,
					),
					func(t *testing.T) {
						base := t.TempDir()
						filename := filepath.Join(
							base,
							"lock.json",
						)
						root := filepath.Join(
							base,
							"skills",
						)
						filesystem := afero.NewMemMapFs()
						if native {
							filesystem = afero.NewOsFs()
						}
						if err := filesystem.MkdirAll(
							filepath.Join(
								root,
								"alpha",
							),
							0o700,
						); err != nil {
							t.Fatal(err)
						}
						marker := filepath.Join(
							root,
							"alpha",
							"marker",
						)
						if err := afero.WriteFile(
							filesystem,
							marker,
							[]byte("old bundle"),
							0o600,
						); err != nil {
							t.Fatal(err)
						}
						var previous []byte
						if existing {
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
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						probe := &lateReplacementFS{
							Fs:       filesystem,
							filename: filename,
							point:    point,
							cancel:   cancel,
						}
						outcome, err := bundlefs.ApplyWithFile(
							ctx,
							probe,
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
							context.Canceled,
						) {
							t.Fatalf(
								"ApplyWithFile = %v, %v",
								outcome,
								err,
							)
						}
						for path, want := range map[string]string{
							filename: "external lock",
							marker:   "old bundle",
						} {
							data, readErr := afero.ReadFile(
								filesystem,
								path,
							)
							if readErr != nil || string(data) != want {
								t.Fatalf(
									"%s = %q, %v; want %q",
									path,
									data,
									readErr,
									want,
								)
							}
						}
						if existing {
							entries, readErr := afero.ReadDir(
								filesystem,
								base,
							)
							if readErr != nil {
								t.Fatal(readErr)
							}
							identified := false
							for _, entry := range entries {
								if !strings.HasPrefix(
									entry.Name(),
									".cpe-backup-",
								) {
									continue
								}
								path := filepath.Join(
									base,
									entry.Name(),
								)
								data, readErr := afero.ReadFile(
									filesystem,
									path,
								)
								if readErr == nil && string(data) == "old lock" && strings.Contains(
									err.Error(),
									path,
								) {
									identified = true
								}
							}
							if !identified {
								t.Fatalf(
									"original recovery file was not retained and identified: %v",
									err,
								)
							}
						}
					},
				)
			}
		}
	}
}

type lateReplacementFS struct {
	afero.Fs
	filename string
	point    string
	cancel   context.CancelFunc
	once     sync.Once
	err      error
}

func (filesystem *lateReplacementFS) replace() error {
	filesystem.once.Do(func() {
		if err := filesystem.Remove(filesystem.filename); err != nil && !errors.Is(
			err,
			fs.ErrNotExist,
		) {
			filesystem.err = err
			return
		}
		filesystem.err = afero.WriteFile(
			filesystem.Fs,
			filesystem.filename,
			[]byte("external lock"),
			0o600,
		)
	})
	return filesystem.err
}

func (filesystem *lateReplacementFS) Open(filename string) (afero.File, error) {
	file, err := filesystem.Fs.Open(filename)
	if err != nil {
		return nil, err
	}
	var action func() error
	if filename == filepath.Dir(filesystem.filename) {
		action = func() error {
			filesystem.cancel()
			if filesystem.point == "after publication" {
				return filesystem.replace()
			}
			return nil
		}
	} else if strings.HasPrefix(
		filepath.Base(filename),
		".cpe-displaced-",
	) && filesystem.point == "during withdrawal" {
		action = filesystem.replace
	}
	if action == nil {
		return file, nil
	}
	return wrapClosingFile(
		file,
		action,
	), nil
}

type closingFile struct {
	afero.File
	action func() error
}

func (file closingFile) Close() error {
	return errors.Join(
		file.File.Close(),
		file.action(),
	)
}

type nativeClosingFile struct {
	closingFile
	descriptor uintptr
}

func (file nativeClosingFile) Fd() uintptr { return file.descriptor }

func wrapClosingFile(file afero.File, action func() error) afero.File {
	wrapped := closingFile{
		File:   file,
		action: action,
	}
	if native, ok := file.(interface{ Fd() uintptr }); ok {
		return nativeClosingFile{
			closingFile: wrapped,
			descriptor:  native.Fd(),
		}
	}
	return wrapped
}
