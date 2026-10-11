//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package skills

import (
	"errors"

	"github.com/spf13/afero"
)

const (
	installedOpenFlags   = 0
	installedParentFlags = 0
)

func openInstalledNative(uintptr, string, string, int) (afero.File, error) {
	return nil, errors.New("native installed content verification is unavailable")
}
