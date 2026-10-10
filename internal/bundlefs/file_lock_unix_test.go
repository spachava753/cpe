//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package bundlefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeFileLockModes(t *testing.T) {
	for _, test := range []struct {
		name       string
		first      guardAccess
		second     guardAccess
		wantSecond bool
	}{
		{
			name:       "readers share read-only descriptors",
			first:      readerGuard,
			second:     readerGuard,
			wantSecond: true,
		},
		{
			name:   "readers exclude writers",
			first:  readerGuard,
			second: writerGuard,
		},
		{
			name:   "writers exclude readers",
			first:  writerGuard,
			second: readerGuard,
		},
		{
			name:   "writers exclude writers",
			first:  writerGuard,
			second: writerGuard,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filename := filepath.Join(
					t.TempDir(),
					"guard",
				)
				if err := os.WriteFile(
					filename,
					nil,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				for index, access := range []guardAccess{
					test.first,
					test.second,
				} {
					flags := os.O_RDONLY
					if access == writerGuard {
						flags = os.O_RDWR
					}
					file, err := os.OpenFile(
						filename,
						flags,
						0o600,
					)
					if err != nil {
						t.Fatal(err)
					}
					defer file.Close()
					locked, err := tryFileLock(
						file.Fd(),
						access,
					)
					if err != nil {
						t.Fatal(err)
					}
					if locked {
						defer unlockFile(file.Fd())
					}
					want := index == 0 || test.wantSecond
					if locked != want {
						t.Fatalf(
							"lock %d = %v; want %v",
							index,
							locked,
							want,
						)
					}
				}
			},
		)
	}
}
