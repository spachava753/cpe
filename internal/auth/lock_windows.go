package auth

import (
	"errors"

	"golang.org/x/sys/windows"
)

func tryCredentialLock(descriptor uintptr) (bool, error) {
	err := windows.LockFileEx(
		windows.Handle(descriptor),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&windows.Overlapped{},
	)
	if errors.Is(
		err,
		windows.ERROR_LOCK_VIOLATION,
	) {
		return false, nil
	}
	return err == nil, err
}

func unlockCredentialFile(descriptor uintptr) error {
	return windows.UnlockFileEx(
		windows.Handle(descriptor),
		0,
		1,
		0,
		&windows.Overlapped{},
	)
}
