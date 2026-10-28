//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package toolruntime

import (
	"os"
	"os/exec"
)

func configureProcess(command *exec.Cmd) func() {
	return func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
}

func exitStatus(state *os.ProcessState) int {
	return state.ExitCode()
}
