package cli

import "github.com/spf13/cobra"

func Root(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "cpe",
		Short:   "Another ACP agent server that comes with builtin TUI",
		Version: version,
	}
}
