//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package toolruntime

import "os/exec"

func configureProcess(command *exec.Cmd) func() {
	return func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
}
