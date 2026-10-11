//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package bundlefs_test

import (
	"testing"

	"golang.org/x/sys/unix"
)

func makeIdentityFIFO(t *testing.T, filename string) {
	t.Helper()
	if err := unix.Mkfifo(
		filename,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func identityOpenIsNonblocking(flags int) bool {
	return flags&unix.O_NONBLOCK != 0 && flags&unix.O_DIRECTORY != 0
}
