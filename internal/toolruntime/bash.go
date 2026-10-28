package toolruntime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// bashRun is the outcome of one bash execution.
type bashRun struct {
	output   tailResult
	timeout  time.Duration
	exitCode int
	timedOut bool
}

// runBash executes script with bash in cwd. stdout and stderr share one pipe,
// preserving their interleaving. The process group is killed when the script
// exits, times out, or ctx is cancelled, so background children never outlive
// the call. An error means bash could not run or ctx was cancelled.
func runBash(ctx context.Context, cwd, script string, timeout time.Duration, sink *tailSink) (bashRun, error) {
	run := bashRun{
		timeout: timeout,
	}
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(
			ctx,
			timeout,
		)
		defer cancel()
	}
	command := exec.CommandContext(
		runCtx,
		"bash",
		"-c",
		script,
	)
	command.Dir = cwd
	// The same writer for both streams makes exec share one pipe.
	command.Stdout = sink
	command.Stderr = sink
	command.WaitDelay = time.Second
	cleanup := configureProcess(command)
	err := command.Run()
	cleanup()
	run.output = sink.finish()
	if ctx.Err() != nil {
		return run, ctx.Err()
	}
	if runCtx.Err() != nil {
		run.timedOut = true
	}
	if command.ProcessState != nil {
		run.exitCode = exitStatus(command.ProcessState)
		return run, nil
	}
	if run.timedOut {
		return run, nil
	}
	return run, fmt.Errorf(
		"run bash: %w",
		err,
	)
}

// diagnostics reports the exit status and timeout outside the output budget.
func (r bashRun) diagnostics() string {
	switch {
	case r.timedOut:
		return fmt.Sprintf(
			"[Timed out after %s; the process group was killed.]",
			r.timeout,
		)
	case r.exitCode != 0:
		return fmt.Sprintf(
			"[Exit status %d]",
			r.exitCode,
		)
	default:
		return ""
	}
}

func parseTimeout(value any) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	seconds, ok := value.(float64)
	if !ok {
		return 0, errors.New("timeout must be a number of seconds")
	}
	if seconds <= 0 || seconds > float64(1<<62)/float64(time.Second) {
		return 0, errors.New("timeout must be a positive number of seconds")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
