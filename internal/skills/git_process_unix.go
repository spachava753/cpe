//go:build unix

package skills

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureGitCancellation(command *exec.Cmd) {
	// Git's transport helpers inherit its process group and output pipes. Killing
	// only Git can leave CombinedOutput waiting indefinitely on those helpers.
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	command.Cancel = func() error {
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
}
