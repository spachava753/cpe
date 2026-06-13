package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/version"
)

// DefaultModel is the process-start snapshot of CPE_MODEL.
// It is used as the default model selector for inspection commands that resolve
// a single model profile outside an ACP session.
var DefaultModel = os.Getenv("CPE_MODEL")

var (
	model                   string
	conversationStoragePath string
	configPath              string
	versionFlag             bool
)

// rootCmd is the executable command hub for CPE.
//
// CPE's primary runtime is the ACP server exposed by "cpe acp serve". The rest
// of the command tree contains local inspection and account/configuration
// helpers for that server runtime.
var rootCmd = &cobra.Command{
	Use:   "cpe",
	Short: "ACP server for AI coding clients",
	Long: `CPE (Chat-based Programming Editor) runs as an Agent Client Protocol
(ACP) server for editor clients such as Zed. Use "cpe acp serve" from an
ACP-compatible client configuration, and use the other commands to inspect model
profiles, MCP servers, and provider account state.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return fmt.Errorf("run CPE through an ACP client with 'cpe acp serve'")
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if versionFlag {
			fmt.Fprintln(cmd.OutOrStdout(), version.Get())
			return nil
		}
		return cmd.Help()
	},
}

// Execute runs the Cobra command tree with process-level signal cancellation.
// It is the top-level boundary between OS process lifecycle handling and command
// execution, and exits with status 1 when command execution returns an error.
func Execute() {
	// Listen for cancellation
	// - in shells for user-initiated interruption SIGINT
	// - in system sent/container environments, SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to YAML configuration file (default: ./cpe.yaml, ~/.config/cpe/cpe.yaml)")
	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Print the version number and exit")
}
