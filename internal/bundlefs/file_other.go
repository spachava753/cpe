//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package bundlefs

import "errors"

func tryFileLock(uintptr) (bool, error) {
	return false, errors.New("native publication locking is unsupported on this platform")
}

func unlockFile(uintptr) error {
	return nil
}

func publishNativeFile(uintptr, string, string) error {
	return errors.New("native file publication is unsupported on this platform")
}
