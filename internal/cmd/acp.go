package cmd

import (
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/acp"
)

// acpCmd is a CLI-only command group; subcommands delegate functionality to
// internal/commands after Cobra argument parsing.
var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Manage CPE configuration",
}

// acpServeCmd parses config, and starts up acp server
var acpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "", // TODO: add short desc
	Long:  ``, // TODO: add long desc
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return acp.Serve(cmd.Context(), acp.ServeOptions{
			ConfigPath: configPath,
			Stdout:     cmd.OutOrStdout(),
			Stderr:     cmd.ErrOrStderr(),
		})
	},
}

func init() {
	acpCmd.AddCommand(acpServeCmd)
	rootCmd.AddCommand(acpCmd)
}
