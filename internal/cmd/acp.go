package cmd

import (
	"fmt"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/acp"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

var (
	acpListPage     uint64 = 1
	acpListPageSize uint64 = 20
)

// acpCmd groups commands for serving ACP and managing persisted sessions.
var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Serve ACP and manage sessions",
	Long: `Run CPE through the Agent Client Protocol (ACP) or inspect and manage
persisted ACP sessions.`,
}

// acpServeCmd starts the stdio ACP server used by editor clients.
var acpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the stdio ACP server",
	Long: `Start CPE's Agent Client Protocol server over stdin/stdout.

Configure your ACP client to launch this command. For example, Zed supports
custom ACP agents through the agent_servers setting:
https://zed.dev/docs/ai/external-agents`,
	Example: `  # Start with discovered config and the centralized session database
  cpe acp serve

  # Start with explicit config and session database paths
  cpe acp serve --config /path/to/cpe.yaml --db-path /path/to/cpeconvo.db`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rawCfg, err := config.LoadRawConfig(configPath)
		if err != nil {
			return fmt.Errorf("could not load config: %w", err)
		}
		store, err := storage.NewConvoDB(cmd.Context(), conversationStoragePath)
		if err != nil {
			return fmt.Errorf("could not open conversation storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		return acp.Serve(cmd.Context(), acp.ServeOptions{
			RawConfig: rawCfg,
			Store:     store,
			Stdout:    cmd.OutOrStdout(),
			Stderr:    cmd.ErrOrStderr(),
			Stdin:     cmd.InOrStdin(),
		})
	},
}

var acpListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List persisted ACP sessions",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewConvoDB(cmd.Context(), conversationStoragePath)
		if err != nil {
			return fmt.Errorf("could not open conversation storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		return acp.ListStoredSessions(cmd.Context(), acp.ListStoredSessionsOptions{
			Store:    store,
			Writer:   cmd.OutOrStdout(),
			Page:     acpListPage,
			PageSize: acpListPageSize,
		})
	},
}

var acpShowCmd = &cobra.Command{
	Use:   "show <session-id>",
	Short: "Show an ACP session as Markdown",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewConvoDB(cmd.Context(), conversationStoragePath)
		if err != nil {
			return fmt.Errorf("could not open conversation storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		return acp.ShowStoredSession(cmd.Context(), acp.ShowStoredSessionOptions{
			Store:     store,
			Writer:    cmd.OutOrStdout(),
			SessionID: acpsdk.SessionId(args[0]),
		})
	},
}

var acpDeleteCmd = &cobra.Command{
	Use:   "delete <session-id>",
	Short: "Delete an ACP session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewConvoDB(cmd.Context(), conversationStoragePath)
		if err != nil {
			return fmt.Errorf("could not open conversation storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		return acp.DeleteStoredSession(cmd.Context(), store, acpsdk.SessionId(args[0]))
	},
}

var acpForkCmd = &cobra.Command{
	Use:   "fork <session-id>",
	Short: "Fork an ACP session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewConvoDB(cmd.Context(), conversationStoragePath)
		if err != nil {
			return fmt.Errorf("could not open conversation storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		return acp.ForkStoredSession(cmd.Context(), acp.ForkStoredSessionOptions{
			Store:     store,
			Writer:    cmd.OutOrStdout(),
			SessionID: acpsdk.SessionId(args[0]),
		})
	},
}

func init() {
	acpListCmd.Flags().Uint64Var(&acpListPage, "page", 1, "Page number (starting at 1)")
	acpListCmd.Flags().Uint64Var(&acpListPageSize, "page-size", 20, "Number of sessions per page (maximum 1000)")

	acpCmd.AddCommand(acpServeCmd)
	acpCmd.AddCommand(acpListCmd)
	acpCmd.AddCommand(acpShowCmd)
	acpCmd.AddCommand(acpDeleteCmd)
	acpCmd.AddCommand(acpForkCmd)
	rootCmd.AddCommand(acpCmd)
}
