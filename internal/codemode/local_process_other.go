//go:build !unix

package codemode

import (
	"errors"
	"os"
	"os/exec"
)

func configureLocalProgramCommand(*exec.Cmd) {}

func interruptLocalProgram(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func killLocalProgram(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
