//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package bundlefs_test

import "testing"

func makeIdentityFIFO(t *testing.T, _ string) {
	t.Helper()
	t.Skip("native FIFO tests require a supported Unix platform")
}

func identityOpenIsNonblocking(int) bool { return false }
