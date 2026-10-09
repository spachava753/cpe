//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package auth

import (
	"errors"

	"golang.org/x/sys/unix"
)

func tryCredentialLock(descriptor uintptr) (bool, error) {
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

func unlockCredentialFile(descriptor uintptr) error {
	return unix.Flock(
		int(descriptor),
		unix.LOCK_UN,
	)
}
