//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package bundlefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// O_DIRECTORY rejects replacements with non-directories; O_NONBLOCK prevents
// opening a special-file stream if the pathname changes after the type check.
const directoryOpenFlags = unix.O_DIRECTORY | unix.O_NONBLOCK

func tryFileLock(descriptor uintptr, access guardAccess) (bool, error) {
	flags := unix.LOCK_SH | unix.LOCK_NB
	if access == writerGuard {
		flags = unix.LOCK_EX | unix.LOCK_NB
	}
	err := unix.Flock(
		int(descriptor),
		flags,
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
