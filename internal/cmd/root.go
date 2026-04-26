package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/version"
)

// DefaultModel is the process-start snapshot of CPE_MODEL.
// It is used as the default value for the --model flag; final model resolution
// still follows config.ResolveConfig precedence rules.
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

	// CLI flag variables for generation parameters (intermediate storage).
	// These are bound to cobra flags; values are only promoted to *gai.GenOpts
	// when the corresponding flag was explicitly set by the user.
	flagMaxTokens        int
	flagTemperature      float64
	flagTopP             float64
	flagTopK             uint
	flagFrequencyPenalty float64
	flagPresencePenalty  float64
	flagN                uint
	flagThinkingBudget   string
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
	RunE: func(cmd *cobra.Command, args []string) error {
		versionFlag, _ := cmd.Flags().GetBool("version")
		if versionFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "cpe version %s\n", version.Get())
			return nil
		}

		var changed commands.GenParamChanges
		cmd.Flags().Visit(func(f *pflag.Flag) {
			switch f.Name {
			case "max-tokens":
				changed.MaxTokens = true
			case "temperature":
				changed.Temperature = true
			case "top-p":
				changed.TopP = true
			case "top-k":
				changed.TopK = true
			case "frequency-penalty":
				changed.FrequencyPenalty = true
			case "presence-penalty":
				changed.PresencePenalty = true
			case "number-of-responses":
				changed.N = true
			case "thinking-budget":
				changed.ThinkingBudget = true
			}
		})

		genParams := commands.BuildGenOpts(commands.GenParamValues{
			MaxTokens:        &flagMaxTokens,
			Temperature:      &flagTemperature,
			TopP:             &flagTopP,
			TopK:             &flagTopK,
			FrequencyPenalty: &flagFrequencyPenalty,
			PresencePenalty:  &flagPresencePenalty,
			N:                &flagN,
			ThinkingBudget:   flagThinkingBudget,
		}, changed)

		return commands.ExecuteRootCLI(cmd.Context(), commands.ExecuteRootCLIOptions{
			Args:            args,
			InputPaths:      input,
			Stdin:           cmd.InOrStdin(),
			SkipStdin:       skipStdin,
			ConfigPath:      configPath,
			ModelRef:        model,
			GenParams:       genParams,
			Timeout:         timeout,
			CustomURL:       customURL,
			ContinueID:      continueID,
			NewConversation: newConversation,
			IncognitoMode:   incognitoMode,
			Stdout:          cmd.OutOrStdout(),
			Stderr:          cmd.ErrOrStderr(),
		})
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
	// Define flags for the root command
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", DefaultModel, "Specify the model to use")
	rootCmd.PersistentFlags().StringVar(&customURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	rootCmd.PersistentFlags().IntVarP(&flagMaxTokens, "max-tokens", "x", 0, "Maximum number of tokens to generate")
	rootCmd.PersistentFlags().Float64VarP(&flagTemperature, "temperature", "t", 0, "Sampling temperature (0.0 - 1.0)")
	rootCmd.PersistentFlags().Float64Var(&flagTopP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	rootCmd.PersistentFlags().UintVar(&flagTopK, "top-k", 0, "Top-k sampling parameter")
	rootCmd.PersistentFlags().Float64Var(&flagFrequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().Float64Var(&flagPresencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().UintVar(&flagN, "number-of-responses", 0, "Number of responses to generate")
	rootCmd.PersistentFlags().StringVarP(&flagThinkingBudget, "thinking-budget", "b", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	rootCmd.PersistentFlags().StringSliceVarP(&input, "input", "i", []string{}, "Specify input files or HTTP(S) URLs to process. Multiple inputs can be provided.")
	rootCmd.PersistentFlags().BoolVarP(&newConversation, "new", "n", false, "Start a new conversation instead of continuing from the last one")
	rootCmd.PersistentFlags().StringVarP(&continueID, "continue", "c", "", "Continue from a specific conversation ID")
	rootCmd.PersistentFlags().BoolVarP(&incognitoMode, "incognito", "G", false, "Run in incognito mode (do not save conversations to storage)")
	rootCmd.PersistentFlags().StringVarP(&timeout, "timeout", "", "", "Specify request timeout duration (e.g. '5m', '30s')")
	rootCmd.PersistentFlags().BoolVar(&skipStdin, "skip-stdin", false, "Skip reading from stdin (useful in scripts)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to unified configuration file (default: ./cpe.yaml, ~/.config/cpe/cpe.yaml)")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}
