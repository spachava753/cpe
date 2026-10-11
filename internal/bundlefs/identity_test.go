package bundlefs_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/bundlefs"
)

func TestSameDirectory(t *testing.T) {
	for _, test := range []struct {
		name           string
		second         string
		native         bool
		unavailable    bool
		hiddenIdentity bool
		openError      bool
		closeError     bool
		fifo           bool
		replacedFIFO   bool
		wantSame       bool
		wantError      bool
	}{
		{
			name:     "same memory directory",
			second:   "first",
			wantSame: true,
		},
		{
			name:     "same native directory",
			native:   true,
			second:   "first",
			wantSame: true,
		},
		{
			name:   "different memory directories",
			second: "second",
		},
		{
			name:   "different native directories",
			native: true,
			second: "second",
		},
		{
			name:   "missing directory",
			second: "missing",
		},
		{
			name:   "regular file is not a directory",
			second: "file",
		},
		{
			name:   "native FIFO is rejected without opening its stream",
			native: true,
			fifo:   true,
			second: "fifo",
		},
		{
			name:         "native replacement with FIFO cannot block directory acquisition",
			native:       true,
			replacedFIFO: true,
			second:       "second",
			wantError:    true,
		},
		{
			name:        "filesystem is required",
			unavailable: true,
			wantError:   true,
		},
		{
			name:           "unknown directory identity fails conservatively",
			hiddenIdentity: true,
			second:         "first",
			wantError:      true,
		},
		{
			name:      "open error is preserved",
			openError: true,
			wantError: true,
		},
		{
			name:       "close error is preserved",
			closeError: true,
			second:     "first",
			wantError:  true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := t.TempDir()
				filesystem := afero.NewMemMapFs()
				if test.native {
					filesystem = afero.NewOsFs()
				}
				for _, name := range []string{
					"first",
					"second",
				} {
					if err := filesystem.MkdirAll(
						filepath.Join(
							root,
							name,
						),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
				}
				if err := afero.WriteFile(
					filesystem,
					filepath.Join(
						root,
						"file",
					),
					[]byte("file"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if test.fifo {
					fifo := filepath.Join(
						root,
						test.second,
					)
					makeIdentityFIFO(
						t,
						fifo,
					)
					filesystem = directoryIdentityFS{
						Fs:        filesystem,
						forbidden: fifo,
					}
				}
				if test.replacedFIFO {
					replaced := filepath.Join(
						root,
						test.second,
					)
					// Verify native FIFO support before installing the acquisition hook.
					makeIdentityFIFO(
						t,
						filepath.Join(
							root,
							"probe",
						),
					)
					filesystem = directoryIdentityFS{
						Fs: filesystem,
						beforeOpen: func(name string, flags int) error {
							if name != replaced {
								return nil
							}
							if !identityOpenIsNonblocking(flags) {
								t.Error("directory acquisition must be directory-only and nonblocking")
								return fs.ErrPermission
							}
							if err := os.Remove(replaced); err != nil {
								return err
							}
							makeIdentityFIFO(
								t,
								replaced,
							)
							return nil
						},
					}
				}
				if test.unavailable {
					filesystem = nil
				} else if test.hiddenIdentity || test.openError || test.closeError {
					filesystem = directoryIdentityFS{
						Fs:         filesystem,
						hidden:     test.hiddenIdentity,
						openError:  test.openError,
						closeError: test.closeError,
					}
				}
				got, err := bundlefs.SameDirectory(
					filesystem,
					filepath.Join(
						root,
						"first",
					),
					filepath.Join(
						root,
						test.second,
					),
				)
				if got != test.wantSame || (err != nil) != test.wantError {
					t.Fatalf(
						"SameDirectory = %v, %v",
						got,
						err,
					)
				}
				if (test.openError || test.closeError) && !errors.Is(
					err,
					fs.ErrPermission,
				) {
					t.Fatalf(
						"permission cause was lost: %v",
						err,
					)
				}
			},
		)
	}
}

type directoryIdentityFS struct {
	afero.Fs
	hidden     bool
	openError  bool
	closeError bool
	forbidden  string
	beforeOpen func(string, int) error
}

func (filesystem directoryIdentityFS) Open(name string) (afero.File, error) {
	return filesystem.OpenFile(
		name,
		os.O_RDONLY,
		0,
	)
}

func (filesystem directoryIdentityFS) OpenFile(name string, flags int, mode fs.FileMode) (afero.File, error) {
	if filesystem.forbidden == name {
		return nil, errors.New("non-directory stream must not be opened")
	}
	if filesystem.beforeOpen != nil {
		if err := filesystem.beforeOpen(
			name,
			flags,
		); err != nil {
			return nil, err
		}
	}
	if filesystem.openError {
		return nil, fs.ErrPermission
	}
	file, err := filesystem.Fs.OpenFile(
		name,
		flags,
		mode,
	)
	if err != nil {
		return nil, err
	}
	return directoryIdentityFile{
		File:       file,
		hidden:     filesystem.hidden,
		closeError: filesystem.closeError,
	}, nil
}

type directoryIdentityFile struct {
	afero.File
	hidden     bool
	closeError bool
}

func (file directoryIdentityFile) Stat() (fs.FileInfo, error) {
	info, err := file.File.Stat()
	if err != nil || !file.hidden {
		return info, err
	}
	return unknownDirectoryInfo{
		info: info,
	}, nil
}

func (file directoryIdentityFile) Close() error {
	err := file.File.Close()
	if file.closeError {
		return errors.Join(
			err,
			fs.ErrPermission,
		)
	}
	return err
}

type unknownDirectoryInfo struct{ info fs.FileInfo }

func (info unknownDirectoryInfo) Name() string       { return info.info.Name() }
func (info unknownDirectoryInfo) Size() int64        { return info.info.Size() }
func (info unknownDirectoryInfo) Mode() fs.FileMode  { return info.info.Mode() }
func (info unknownDirectoryInfo) ModTime() time.Time { return info.info.ModTime() }
func (info unknownDirectoryInfo) IsDir() bool        { return info.info.IsDir() }
func (info unknownDirectoryInfo) Sys() any           { return nil }
