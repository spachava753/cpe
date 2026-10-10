package bundlefs

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func tryFileLock(descriptor uintptr) (bool, error) {
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

func unlockFile(descriptor uintptr) error {
	return windows.UnlockFileEx(
		windows.Handle(descriptor),
		0,
		1,
		0,
		&windows.Overlapped{},
	)
}

func publishNativeFile(directory uintptr, staged string, filename string) error {
	buffer := make(
		[]uint16,
		512,
	)
	length, err := windows.GetFinalPathNameByHandle(
		windows.Handle(directory),
		&buffer[0],
		uint32(len(buffer)),
		0,
	)
	if err != nil {
		return err
	}
	if length >= uint32(len(buffer)) {
		buffer = make(
			[]uint16,
			length+1,
		)
		length, err = windows.GetFinalPathNameByHandle(
			windows.Handle(directory),
			&buffer[0],
			uint32(len(buffer)),
			0,
		)
		if err != nil {
			return err
		}
	}
	if length >= uint32(len(buffer)) {
		return errors.New("native publication directory path changed")
	}
	parent := windows.UTF16ToString(buffer[:length])
	from, err := windows.UTF16PtrFromString(filepath.Join(
		parent,
		staged,
	))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(
		parent,
		filename,
	))
	if err != nil {
		return err
	}
	return windows.MoveFile(
		from,
		to,
	)
}
