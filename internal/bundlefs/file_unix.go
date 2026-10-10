//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package bundlefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

func tryFileLock(descriptor uintptr) (bool, error) {
	err := unix.Flock(
		int(descriptor),
		unix.LOCK_EX|unix.LOCK_NB,
	)
	if errors.Is(
		err,
		unix.EWOULDBLOCK,
	) || errors.Is(
		err,
		unix.EAGAIN,
	) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(descriptor uintptr) error {
	return unix.Flock(
		int(descriptor),
		unix.LOCK_UN,
	)
}

func publishNativeFile(directory uintptr, staged string, filename string) error {
	return unix.Linkat(
		int(directory),
		staged,
		int(directory),
		filename,
		0,
	)
}
