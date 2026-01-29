package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/version"
)

// DefaultModel holds the global default LLM model for the CLI.
// It is set at process startup from CPE_MODEL env var (or empty if unset).
var DefaultModel = os.Getenv("CPE_MODEL")

var (
	model           string
	customURL       string
	input           []string
	newConversation bool
	continueID      string
	incognitoMode   bool
	timeout         string
	skipStdin       bool
	configPath      string
	verboseSubagent bool

	genParams gai.GenOpts
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cpe [flags] [prompt]",
	Short: "Chat-based Programming Editor",
	Long: `CPE (Chat-based Programming Editor) is a powerful command-line tool that enables 
developers to leverage AI for codebase analysis, modification, and improvement 
through natural language interactions.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if version flag is set
		versionFlag, _ := cmd.Flags().GetBool("version")
		if versionFlag {
			fmt.Printf("cpe version %s\n", version.Get())
			return nil
		}

		// Delegate to business logic
		return commands.ExecuteRoot(cmd.Context(), commands.ExecuteRootOptions{
			Args:            args,
			InputPaths:      input,
			Stdin:           os.Stdin,
			SkipStdin:       skipStdin,
			ConfigPath:      configPath,
			ModelRef:        model,
			CustomURL:       customURL,
			GenParams:       &genParams,
			Timeout:         timeout,
			ContinueID:      continueID,
			NewConversation: newConversation,
			IncognitoMode:   incognitoMode,
			Stderr:          os.Stderr,
			VerboseSubagent: verboseSubagent,
		})
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
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
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", DefaultModel, "Specify the model to use")
	rootCmd.PersistentFlags().StringVar(&customURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	rootCmd.PersistentFlags().IntVarP(&genParams.MaxGenerationTokens, "max-tokens", "x", 0, "Maximum number of tokens to generate")
	rootCmd.PersistentFlags().Float64VarP(&genParams.Temperature, "temperature", "t", 0, "Sampling temperature (0.0 - 1.0)")
	rootCmd.PersistentFlags().Float64Var(&genParams.TopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	rootCmd.PersistentFlags().UintVar(&genParams.TopK, "top-k", 0, "Top-k sampling parameter")
	rootCmd.PersistentFlags().Float64Var(&genParams.FrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().Float64Var(&genParams.PresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().UintVar(&genParams.N, "number-of-responses", 0, "Number of responses to generate")
	rootCmd.PersistentFlags().StringVarP(&genParams.ThinkingBudget, "thinking-budget", "b", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	rootCmd.PersistentFlags().StringSliceVarP(&input, "input", "i", []string{}, "Specify input files or HTTP(S) URLs to process. Multiple inputs can be provided.")
	rootCmd.PersistentFlags().BoolVarP(&newConversation, "new", "n", false, "Start a new conversation instead of continuing from the last one")
	rootCmd.PersistentFlags().StringVarP(&continueID, "continue", "c", "", "Continue from a specific conversation ID")
	rootCmd.PersistentFlags().BoolVarP(&incognitoMode, "incognito", "G", false, "Run in incognito mode (do not save conversations to storage)")
	rootCmd.PersistentFlags().StringVarP(&timeout, "timeout", "", "", "Specify request timeout duration (e.g. '5m', '30s')")
	rootCmd.PersistentFlags().BoolVar(&skipStdin, "skip-stdin", false, "Skip reading from stdin (useful in scripts)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to unified configuration file (default: ./cpe.yaml, ~/.config/cpe/cpe.yaml)")

	// Verbose subagent flag with env var fallback
	defaultVerbose := os.Getenv("CPE_VERBOSE_SUBAGENT") == "true" || os.Getenv("CPE_VERBOSE_SUBAGENT") == "1"
	rootCmd.PersistentFlags().BoolVar(&verboseSubagent, "verbose-subagent", defaultVerbose, "Show verbose subagent output including full tool payloads and results")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}
