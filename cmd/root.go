package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spf13/cobra"
)

var (
	// Flags for the root command
	model             string
	customURL         string
	maxTokens         int
	temperature       float64
	topP              float64
	topK              int
	frequencyPenalty  float64
	presencePenalty   float64
	numberOfResponses int
	thinkingBudget    string
	input             []string
	newConversation   bool
	continueID        string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cpe [flags] [prompt]",
	Short: "Chat-based Programming Editor",
	Long: `CPE (Chat-based Programming Editor) is a powerful command-line tool that enables 
developers to leverage AI for codebase analysis, modification, and improvement 
through natural language interactions.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Check if version flag is set
		versionFlag, _ := cmd.Flags().GetBool("version")
		if versionFlag {
			fmt.Printf("cpe version %s\n", getVersion())
			return
		}

		// Initialize the executor and run the main functionality
		executeRootCommand(cmd, args)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// getVersion returns the version of the application from build info
func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown version)"
}

func init() {
	// Define flags for the root command
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "", "Specify the model to use")
	rootCmd.PersistentFlags().StringVar(&customURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	rootCmd.PersistentFlags().IntVarP(&maxTokens, "max-tokens", "x", 0, "Maximum number of tokens to generate")
	rootCmd.PersistentFlags().Float64VarP(&temperature, "temperature", "t", 0, "Sampling temperature (0.0 - 1.0)")
	rootCmd.PersistentFlags().Float64Var(&topP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	rootCmd.PersistentFlags().IntVar(&topK, "top-k", 0, "Top-k sampling parameter")
	rootCmd.PersistentFlags().Float64Var(&frequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().Float64Var(&presencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().IntVar(&numberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	rootCmd.PersistentFlags().StringVarP(&thinkingBudget, "thinking-budget", "b", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	rootCmd.PersistentFlags().StringSliceVarP(&input, "input", "i", []string{}, "Specify input files to process. Multiple files can be provided.")
	rootCmd.PersistentFlags().BoolVarP(&newConversation, "new", "n", false, "Start a new conversation instead of continuing from the last one")
	rootCmd.PersistentFlags().StringVarP(&continueID, "continue", "c", "", "Continue from a specific conversation ID")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}

// executeRootCommand handles the main functionality of the root command
func executeRootCommand(cmd *cobra.Command, args []string) {
	// Initialize ignorer
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load ignore files: %v\n", err)
		os.Exit(1)
	}
	if ignorer == nil {
		fmt.Fprintf(os.Stderr, "Error: git ignorer was nil\n")
		os.Exit(1)
	}

	// TODO: execute root command
}

// getCustomURL returns the custom URL to use based on the following precedence:
// 1. Command-line flag (-custom-url)
// 2. Model-specific environment variable (CPE_MODEL_NAME_URL)
// 3. General custom URL environment variable (CPE_CUSTOM_URL)
func getCustomURL(flagURL string, modelName string) string {
	// Start with the flag value
	urlVal := flagURL

	// Check model-specific env var if we have a model name
	if modelName != "" {
		envVarName := fmt.Sprintf("CPE_%s_URL", strings.ToUpper(strings.ReplaceAll(modelName, "-", "_")))
		if modelEnvURL := os.Getenv(envVarName); urlVal == "" && modelEnvURL != "" {
			urlVal = modelEnvURL
		}
	}

	// Finally, check the general custom URL env var
	if envURL := os.Getenv("CPE_CUSTOM_URL"); urlVal == "" && envURL != "" {
		urlVal = envURL
	}

	return urlVal
}
