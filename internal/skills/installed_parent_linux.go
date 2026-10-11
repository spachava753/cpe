//go:build linux

package skills

import "golang.org/x/sys/unix"

// O_PATH needs search permission, not read permission, on ancestor directories.
const installedParentFlags = installedOpenFlags | unix.O_DIRECTORY | unix.O_PATH
