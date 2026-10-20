//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package toolruntime

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Subprocesses are invocation-owned, including descendants that retain pipes.
func configureProcess(command *exec.Cmd) func() {
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	kill := func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(
			-command.Process.Pid,
			syscall.SIGKILL,
		)
		if errors.Is(
			err,
			syscall.ESRCH,
		) {
			return os.ErrProcessDone
		}
		return err
	}
	command.Cancel = kill
	return func() { _ = kill() }
}
