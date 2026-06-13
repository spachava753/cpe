package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/acp"
)

// acpCmd groups commands for running CPE through the Agent Client Protocol.
var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Run CPE as an ACP server",
	Long: `Run CPE through the Agent Client Protocol (ACP).

CPE is meant to be launched by an ACP-compatible client, such as Zed, which
starts the server process and communicates with it over stdio JSON-RPC.`,
}

// acpServeCmd starts the stdio ACP server used by editor clients.
var acpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the stdio ACP server",
	Long: `Start CPE's Agent Client Protocol server over stdin/stdout.

Configure your ACP client to launch this command. For example, Zed supports
custom ACP agents through the agent_servers setting:
https://zed.dev/docs/ai/external-agents`,
	Example: `  # Start with discovered config and default ./.cpeconvo session database
  cpe acp serve

  # Start with explicit config and session database paths
  cpe acp serve --config /path/to/cpe.yaml --db-path /path/to/cpeconvo.db`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return acp.Serve(cmd.Context(), acp.ServeOptions{
			ConfigPath: configPath,
			DbPath:     conversationStoragePath,
			Stdout:     cmd.OutOrStdout(),
			Stderr:     cmd.ErrOrStderr(),
			Stdin:      cmd.InOrStdin(),
		})
	},
}

func init() {
	acpServeCmd.Flags().StringVar(&conversationStoragePath, "db-path", os.Getenv("CPE_DB_PATH"), "Path to ACP session SQLite database (default: ./.cpeconvo, env: CPE_DB_PATH)")

	acpCmd.AddCommand(acpServeCmd)
	rootCmd.AddCommand(acpCmd)
}
