//go:build !unix

package config

import (
	"context"
	"os/exec"
)

// templateCommand cancels the shell on platforms without Unix process groups.
// The caller bounds pipe draining in case descendants retain output handles.
func templateCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "bash", "-c", command)
}
