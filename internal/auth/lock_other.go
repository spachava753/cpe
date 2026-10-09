//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package auth

import "errors"

func tryCredentialLock(uintptr) (bool, error) {
	return false, errors.New("native credential locking is unsupported on this platform")
}

func unlockCredentialFile(uintptr) error {
	return nil
}
