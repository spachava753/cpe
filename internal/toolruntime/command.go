package toolruntime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/spachava753/dyson"
)

// localCommandRunner returns complete captured streams to the script.
// TODO: Revisit the memory cost of buffering large command streams in full.
// Commands inherit the current process environment unless Env is explicitly supplied.
type localCommandRunner struct{}

func (localCommandRunner) RunCommand(ctx context.Context, command dyson.Command) (dyson.CommandResult, error) {
	if len(command.Args) == 0 {
		return dyson.CommandResult{}, errors.New("command arguments are empty")
	}
	args := command.Args
	if command.Shell {
		args = append(
			[]string{
				"sh",
				"-c",
			},
			args...,
		)
	}
	process := exec.CommandContext(
		ctx,
		args[0],
		args[1:]...,
	)
	process.Dir = command.Dir
	process.Env = command.Env
	process.WaitDelay = time.Second
	cleanup := configureProcess(process)
	defer cleanup()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	process.Stdout = stdout
	process.Stderr = stderr
	if command.Stdout == dyson.StreamDiscard {
		process.Stdout = io.Discard
	}
	if command.Stderr == dyson.StreamDiscard {
		process.Stderr = io.Discard
	}
	if command.Stderr == dyson.StreamStdout {
		process.Stderr = process.Stdout
	}
	if command.Stdin == dyson.StreamPipe {
		process.Stdin = bytes.NewReader(command.Input)
	}
	err := process.Run()
	result := dyson.CommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		result.ReturnCode = exit.ExitCode()
		return result, nil
	}
	return result, err
}
