package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/db"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spf13/cobra"
)

var (
	// Flags for the root command
	model              string
	customURL          string
	maxTokens          int
	temperature        float64
	topP               float64
	topK               int
	frequencyPenalty   float64
	presencePenalty    float64
	numberOfResponses  int
	thinkingBudget     string
	input              bool
	newConversation    bool
	continueID         string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cpe [flags] [prompt]",
	Short: "Chat-based Programming Editor",
	Long: `CPE (Chat-based Programming Editor) is a powerful command-line tool that enables 
developers to leverage AI for codebase analysis, modification, and improvement 
through natural language interactions.`,
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
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "", fmt.Sprintf("Specify the model to use. Supported models: %s", strings.Join(getModelKeys(), ", ")))
	rootCmd.PersistentFlags().StringVar(&customURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 0, "Maximum number of tokens to generate")
	rootCmd.PersistentFlags().Float64VarP(&temperature, "temperature", "t", 0, "Sampling temperature (0.0 - 1.0)")
	rootCmd.PersistentFlags().Float64Var(&topP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	rootCmd.PersistentFlags().IntVar(&topK, "top-k", 0, "Top-k sampling parameter")
	rootCmd.PersistentFlags().Float64Var(&frequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().Float64Var(&presencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().IntVar(&numberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	rootCmd.PersistentFlags().StringVar(&thinkingBudget, "thinking-budget", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	rootCmd.PersistentFlags().BoolVarP(&input, "input", "i", false, "When provided, all arguments except the last one are treated as input files that must exist. The last argument is either a file path or a prompt text")
	rootCmd.PersistentFlags().BoolVarP(&newConversation, "new", "n", false, "Start a new conversation instead of continuing from the last one")
	rootCmd.PersistentFlags().StringVarP(&continueID, "continue", "c", "", "Continue from a specific conversation ID")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}

// getModelKeys returns a slice of model keys from the ModelConfigs map
func getModelKeys() []string {
	var keys []string
	for k := range agent.ModelConfigs {
		keys = append(keys, k)
	}
	return keys
}

// executeRootCommand handles the main functionality of the root command
func executeRootCommand(cmd *cobra.Command, args []string) {
	var inputs []agent.Input
	var err error
	
	// Read input from stdin, files, or arguments
	inputs, err = readInput(input, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

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

	// Get model from environment or default if not specified
	if model == "" {
		model = agent.GetModelFromFlagsOrDefault(agent.ModelOptions{})
	}

	// Initialize the executor
	executor, err := agent.InitExecutor(log.Default(), agent.ModelOptions{
		Model:              model,
		CustomURL:          customURL,
		MaxTokens:          maxTokens,
		Temperature:        temperature,
		TopP:               topP,
		TopK:               topK,
		FrequencyPenalty:   frequencyPenalty,
		PresencePenalty:    presencePenalty,
		NumberOfResponses:  numberOfResponses,
		ThinkingBudget:     thinkingBudget,
		Continue:           continueID,
		New:                newConversation,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get model config to validate input types
	modelConfig, ok := agent.ModelConfigs[model]
	if !ok {
		// If no model flag, try to get model from conversation
		if !newConversation {
			// Initialize conversation manager
			dbPath := ".cpeconvo"
			convoManager, err := conversation.NewManager(dbPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer convoManager.Close()

			// Get conversation
			var conv *db.Conversation
			if continueID != "" {
				conv, err = convoManager.GetConversation(context.Background(), continueID)
			} else {
				conv, err = convoManager.GetLatestConversation(context.Background())
			}
			if err == nil {
				// Find model alias by model name
				for alias, cfg := range agent.ModelConfigs {
					if cfg.Name == conv.Model {
						modelConfig = cfg
						model = alias // Set the model name
						ok = true
						break
					}
				}
			}
		}

		// If still not found, get model from flags/env/default
		if !ok {
			modelName := agent.GetModelFromFlagsOrDefault(agent.ModelOptions{
				Model: model,
			})
			modelConfig, ok = agent.ModelConfigs[modelName]
			if !ok {
				// Unknown model, default to text only
				modelConfig = agent.ModelConfig{
					Name:            modelName,
					IsKnown:         false,
					SupportedInputs: []agent.InputType{agent.InputTypeText},
				}
			}
		}
	}

	// Validate input types against model capabilities
	for _, input := range inputs {
		if slices.Contains(modelConfig.SupportedInputs, input.Type) {
			continue
		}
		fmt.Fprintf(os.Stderr, "Error: model %s does not support input type %s (file: %s)\n", 
			model, string(input.Type), input.FilePath)
		os.Exit(1)
	}

	// Execute the model
	if err := executor.Execute(inputs); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// readInput reads input from stdin, files, or arguments
func readInput(inputFlag bool, args []string) ([]agent.Input, error) {
	var inputs []agent.Input

	// Check if there is any input from stdin by checking if stdin is a pipe or redirection
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Stdin has data available
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("error reading from stdin: %w", err)
		}
		if len(content) > 0 {
			inputs = append(inputs, agent.Input{
				Type: agent.InputTypeText,
				Text: string(content),
			})
		}
	}

	// Check if there are input files from command line arguments
	if inputFlag {
		if len(args) < 1 {
			return nil, fmt.Errorf("when using --input flag, need at least one input file")
		}
		// All arguments are treated as input files, except the last one if it's not a file
		lastIdx := len(args)
		lastArg := args[lastIdx-1]
		if _, err := os.Stat(lastArg); err != nil {
			// Last argument doesn't exist as a file, treat it as prompt text
			lastIdx--
			inputs = append(inputs, agent.Input{
				Type: agent.InputTypeText,
				Text: lastArg,
			})
		}
		// Process all other arguments as files
		for _, path := range args[:lastIdx] {
			// Check if file exists
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("input file does not exist: %s", path)
			}
			
			inputType, err := agent.DetectInputType(path)
			if err != nil {
				return nil, fmt.Errorf("error detecting input type for file %s: %w", path, err)
			}

			if inputType == agent.InputTypeText {
				// For text files, read the content and use it as text input
				content, err := os.ReadFile(path)
				if err != nil {
					return nil, fmt.Errorf("error reading file %s: %w", path, err)
				}
				inputs = append(inputs, agent.Input{
					Type: agent.InputTypeText,
					Text: string(content),
				})
			} else {
				// For non-text files, pass the file path
				inputs = append(inputs, agent.Input{
					Type:     inputType,
					FilePath: path,
				})
			}
		}
	} else if len(args) > 0 {
		// Without --input flag, all arguments are combined into a single prompt
		prompt := strings.Join(args, " ")
		inputs = append(inputs, agent.Input{
			Type: agent.InputTypeText,
			Text: prompt,
		})
	}

	if len(inputs) == 0 {
		return nil, fmt.Errorf("no input provided. Please provide input via stdin, input file, or as a command line argument")
	}

	return inputs, nil
}