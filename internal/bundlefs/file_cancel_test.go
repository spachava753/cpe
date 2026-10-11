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

func TestPublicationCancellation(t *testing.T) {
	for _, native := range []bool{
		false,
		true,
	} {
		for _, existing := range []bool{
			false,
			true,
		} {
			for _, placeholderFailure := range []bool{
				false,
				true,
			} {
				t.Run(
					fmt.Sprintf(
						"native=%v/existing=%v/placeholderFailure=%v",
						native,
						existing,
						placeholderFailure,
					),
					func(t *testing.T) {
						base := t.TempDir()
						root := filepath.Join(
							base,
							"skills",
						)
						filename := filepath.Join(
							base,
							"lock.json",
						)
						filesystem := afero.NewMemMapFs()
						if native {
							filesystem = afero.NewOsFs()
						}
						if err := filesystem.MkdirAll(
							base,
							0o700,
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
						probe := &canceledPublicationFS{
							Fs:                 filesystem,
							filename:           filename,
							cancel:             cancel,
							placeholderFailure: placeholderFailure,
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
						data, readErr := afero.ReadFile(
							filesystem,
							filename,
						)
						if placeholderFailure {
							if readErr != nil || string(data) != "new lock" || !strings.Contains(
								err.Error(),
								"published file remains at "+filename,
							) {
								t.Fatalf(
									"surviving lock was not identified: %q, %v, %v",
									data,
									readErr,
									err,
								)
							}
							if existing && !strings.Contains(
								err.Error(),
								"original recovery file retained at",
							) {
								t.Fatalf(
									"original backup omitted: %v",
									err,
								)
							}
						} else if existing {
							if readErr != nil || string(data) != "old lock" {
								t.Fatalf(
									"own publication was misidentified: %q, %v, %v",
									data,
									readErr,
									err,
								)
							}
						} else if !errors.Is(
							readErr,
							fs.ErrNotExist,
						) {
							t.Fatalf(
								"first publication was not removed: %q, %v",
								data,
								readErr,
							)
						}
						if exists, statErr := afero.Exists(
							filesystem,
							filepath.Join(
								root,
								"alpha",
							),
						); statErr != nil || exists {
							t.Fatalf(
								"bundle was not rolled back: %v, %v",
								exists,
								statErr,
							)
						}
					},
				)
			}
		}
	}
}

// Hide pathname-derived IDs for staged files and consume the staging name after
// publication. Handle-based Stat must supply ownership, as it does on Windows.
type canceledPublicationFS struct {
	afero.Fs
	filename           string
	cancel             context.CancelFunc
	placeholderFailure bool
	source             string
	once               sync.Once
	err                error
}

func (filesystem *canceledPublicationFS) LstatIfPossible(filename string) (fs.FileInfo, bool, error) {
	info, err := bundlefs.Lstat(
		filesystem.Fs,
		filename,
	)
	if err == nil && strings.HasPrefix(
		filepath.Base(filename),
		".cpe-file-",
	) {
		return opaquePathInfo{
			FileInfo: info,
		}, true, nil
	}
	return info, true, err
}

type opaquePathInfo struct{ fs.FileInfo }

func (opaquePathInfo) Sys() any { return nil }

func (filesystem *canceledPublicationFS) Open(filename string) (afero.File, error) {
	file, err := filesystem.Fs.Open(filename)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(
		filepath.Base(filename),
		".cpe-file-",
	) {
		filesystem.source = filename
	}
	if filename == filepath.Dir(filesystem.filename) {
		return wrapClosingFile(
			file,
			func() error {
				filesystem.once.Do(func() {
					filesystem.err = filesystem.Remove(filesystem.source)
					if errors.Is(
						filesystem.err,
						fs.ErrNotExist,
					) {
						filesystem.err = nil
					}
					filesystem.cancel()
				})
				return filesystem.err
			},
		), nil
	}
	return file, nil
}

func (filesystem *canceledPublicationFS) OpenFile(filename string, flags int, mode fs.FileMode) (afero.File, error) {
	file, err := filesystem.Fs.OpenFile(
		filename,
		flags,
		mode,
	)
	if err == nil && filesystem.placeholderFailure && strings.HasPrefix(
		filepath.Base(filename),
		".cpe-displaced-",
	) {
		return wrapClosingFile(
			file,
			func() error { return fs.ErrPermission },
		), nil
	}
	return file, err
}
