//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package skills

import "golang.org/x/sys/unix"

const installedParentFlags = installedOpenFlags | unix.O_DIRECTORY
