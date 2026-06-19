//go:build unix

package codemode

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureLocalProgramCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptLocalProgram(process *os.Process) error {
	return signalLocalProgramGroup(process, syscall.SIGINT)
}

func killLocalProgram(process *os.Process) error {
	return signalLocalProgramGroup(process, syscall.SIGKILL)
}

func signalLocalProgramGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return nil
	}
	if err := syscall.Kill(-process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
