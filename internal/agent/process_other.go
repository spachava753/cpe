//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package agent

import "os/exec"

func configureProcess(command *exec.Cmd) func() {
	return func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
}
