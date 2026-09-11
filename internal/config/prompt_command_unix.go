//go:build unix

package config

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// templateCommand isolates the shell and its pipeline in a process group so
// cancellation stops descendants as well as the shell.
func templateCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd
}
