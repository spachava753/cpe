package cli

import "github.com/spf13/cobra"

func Root(version string) *cobra.Command {
	r := &cobra.Command{
		Use:     "cpe",
		Short:   "Another ACP agent server that comes with builtin TUI",
		Version: version,
	}
	skillsCmd(r)
	return r
}
