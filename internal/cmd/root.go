package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// DefaultModel is the process-start snapshot of CPE_MODEL.
// It is used as the default value for the --model flag; a selected model is required.
var DefaultModel = os.Getenv("CPE_MODEL")

var (
	model                   string
	input                   []string
	newConversation         bool
	continueID              string
	incognitoMode           bool
	timeout                 string
	skipStdin               bool
	conversationStoragePath string
	configPath              string
	flagThinkingBudget      string
)

// rootCmd is the CLI entrypoint for prompt execution.
//
// Responsibility split:
//   - internal/cmd: parse flags/env and map Cobra state into plain option
//     structs.
//   - internal/commands: resolve runtime dependencies and execute business logic
//     without Cobra coupling.
var rootCmd = &cobra.Command{
	Use:   "cpe [flags] [prompt]",
	Short: "Chat-based Programming Editor",
	Long: `CPE (Chat-based Programming Editor) is a powerful command-line tool that enables
developers to leverage AI for codebase analysis, modification, and improvement
through natural language interactions.`,
	Args: cobra.ArbitraryArgs,
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
	// Define flags for the root command
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", DefaultModel, "Specify the model to use (required unless CPE_MODEL is set)")
	rootCmd.PersistentFlags().StringVarP(&flagThinkingBudget, "thinking-budget", "b", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	rootCmd.PersistentFlags().StringSliceVarP(&input, "input", "i", []string{}, "Specify input files or HTTP(S) URLs to process. Multiple inputs can be provided.")
	rootCmd.PersistentFlags().BoolVarP(&newConversation, "new", "n", false, "Start a new conversation instead of continuing from the last one")
	rootCmd.PersistentFlags().StringVarP(&continueID, "continue", "c", "", "Continue from a specific conversation ID")
	rootCmd.PersistentFlags().BoolVarP(&incognitoMode, "incognito", "G", false, "Run in incognito mode (do not save conversations to storage)")
	rootCmd.PersistentFlags().StringVarP(&timeout, "timeout", "", "", "Specify request timeout duration (e.g. '5m', '30s')")
	rootCmd.PersistentFlags().BoolVar(&skipStdin, "skip-stdin", false, "Skip reading from stdin (useful in scripts)")
	rootCmd.PersistentFlags().StringVar(&conversationStoragePath, "db-path", os.Getenv("CPE_DB_PATH"), "Path to conversation SQLite database (default: ./.cpeconvo, env: CPE_DB_PATH)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to YAML configuration file (default: ./cpe.yaml, ~/.config/cpe/cpe.yaml)")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}
