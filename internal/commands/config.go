package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/spachava753/cpe/internal/config"
)

// ConfigLintOptions contains parameters for config validation
type ConfigLintOptions struct {
	Config config.Config
	Writer io.Writer
}

// ConfigLint validates a configuration file
func ConfigLint(ctx context.Context, opts ConfigLintOptions) error {
	// Config is already loaded and validated, just report the results
	fmt.Fprintf(opts.Writer, "✓ Configuration is valid\n")
	fmt.Fprintf(opts.Writer, "  Models: %d\n", len(opts.Config.Models))

	if len(opts.Config.MCPServers) > 0 {
		fmt.Fprintf(opts.Writer, "  MCP Servers: %d\n", len(opts.Config.MCPServers))
	}

	if opts.Config.GetDefaultModel() != "" {
		fmt.Fprintf(opts.Writer, "  Default Model: %s\n", opts.Config.GetDefaultModel())
	}

	return nil
}
