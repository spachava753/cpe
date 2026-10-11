//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package skills

import (
	"os"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

const installedOpenFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW

func openInstalledNative(parent uintptr, name string, filename string, flags int) (afero.File, error) {
	descriptor, err := unix.Openat(
		int(parent),
		name,
		flags|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(
		uintptr(descriptor),
		filename,
	), nil
}
