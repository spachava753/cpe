package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

func configureGitCancellation(command *exec.Cmd) {
	command.Cancel = func() error {
		// taskkill /T terminates Git and its owned transport descendants. Use a
		// separate, bounded context because the acquisition context is cancelled.
		ctx, cancel := context.WithTimeout(
			context.Background(),
			time.Second,
		)
		defer cancel()
		taskkill := filepath.Join(
			os.Getenv("SystemRoot"),
			"System32",
			"taskkill.exe",
		)
		err := exec.CommandContext(
			ctx,
			taskkill,
			"/PID",
			strconv.Itoa(command.Process.Pid),
			"/T",
			"/F",
		).Run()
		if err != nil {
			return command.Process.Kill()
		}
		return nil
	}
}
