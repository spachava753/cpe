package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gabriel-vasile/mimetype"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
	"os"
	"runtime/debug"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if version flag is set
		versionFlag, _ := cmd.Flags().GetBool("version")
		if versionFlag {
			fmt.Printf("cpe version %s\n", getVersion())
			return nil
		}

		// Initialize the executor and run the main functionality
		return executeRootCommand(args)
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
func executeRootCommand(args []string) error {
	// Initialize ignorer
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		return fmt.Errorf("failed to load ignore files: %w", err)
	}
	if ignorer == nil {
		return errors.New("git ignorer was nil")
	}

	userBlocks, err := processUserInput(args)
	if err != nil {
		return fmt.Errorf("could not process user input: %w", err)
	}

	// If no input was provided, print help
	if len(userBlocks) == 0 {
		return errors.New("empty input")
	}

	// Always use .cpeconvo as the DB path
	dbPath := ".cpeconvo"

	// Initialize or open the database through the storage package
	dialogStorage, err := storage.InitDialogStorage(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize dialog storage: %w", err)
	}
	defer dialogStorage.Close()

	// Get most recent message
	if continueID == "" && !newConversation {
		continueID, err = dialogStorage.GetMostRecentUserMessageId(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get most recent message: %w", err)
		}
	}

	// Get default model from environment variable
	defaultModel := os.Getenv("CPE_MODEL")
	if defaultModel == "" {
		defaultModel = anthropic.ModelClaude3_7SonnetLatest // Default model if not specified
	}

	if model == "" {
		model = defaultModel
	}

	customURL = getCustomURL(customURL)
	// Create the underlying generator based on the model name
	baseGenerator, err := agent.InitGenerator(model, customURL)
	if err != nil {
		return fmt.Errorf("failed to create base generator: %w", err)
	}

	// Create the tool generator
	toolGen := &gai.ToolGenerator{
		G: baseGenerator,
	}

	// Register all the necessary tools
	if err = agent.RegisterTools(toolGen); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}

	userMessage := gai.Message{
		Role:   gai.User,
		Blocks: userBlocks,
	}

	// Create full dialog with parent message if available
	dialog := gai.Dialog{
		userMessage,
	}

	if !newConversation {
		dialog, err = dialogStorage.GetDialogForUserMessage(context.Background(), continueID)
		if err != nil {
			return fmt.Errorf("failed to get previous dialog: %w", err)
		}
		dialog = append(dialog, userMessage)
	}

	// Create a generator function that returns generation options
	genOptionsFunc := func(d gai.Dialog) *gai.GenOpts {
		opts := &gai.GenOpts{}
		if maxTokens > 0 {
			opts.MaxGenerationTokens = maxTokens
		}
		if temperature > 0 {
			opts.Temperature = temperature
		}
		if topP > 0 {
			opts.TopP = topP
		}
		if topK > 0 {
			opts.TopK = uint(topK)
		}
		if frequencyPenalty != 0 {
			opts.FrequencyPenalty = frequencyPenalty
		}
		if presencePenalty != 0 {
			opts.PresencePenalty = presencePenalty
		}
		if numberOfResponses > 0 {
			opts.N = uint(numberOfResponses)
		}
		return opts
	}

	result, err := toolGen.Generate(context.Background(), dialog, genOptionsFunc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating response: %v\n", err)
		os.Exit(1)
	}

	// Save user message
	userMsgID, err := dialogStorage.SaveMessage(context.Background(), userMessage, continueID, "")
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Save assistant messages
	assistantMsgs := result[len(dialog):]

	parentId := userMsgID
	for _, assistantMsg := range assistantMsgs {
		printMessage(assistantMsg)
		parentId, err = dialogStorage.SaveMessage(context.Background(), assistantMsg, parentId, "")
		if err != nil {
			return fmt.Errorf("failed to save message: %w", err)
		}
	}

	return nil
}

// processUserInput processes and combines user input from all available sources
func processUserInput(args []string) ([]gai.Block, error) {
	var userBlocks []gai.Block

	//// Get input from stdin if available (non-blocking)
	//stdinStat, err := os.Stdin.Stat()
	//if err != nil {
	//	return nil, fmt.Errorf("failed to check stdin: %w", err)
	//}
	//
	//// If stdin has data, read it
	//if (stdinStat.Mode() & os.ModeCharDevice) == 0 {
	//	stdinBytes, err := io.ReadAll(os.Stdin)
	//	if err != nil {
	//		return nil, fmt.Errorf("failed to read from stdin: %w", err)
	//	}
	//	if len(stdinBytes) > 0 {
	//		userBlocks = append(userBlocks, gai.Block{
	//			BlockType:    "text",
	//			ModalityType: gai.Text,
	//			MimeType:     "text/plain",
	//			Content:      gai.Str(stdinBytes),
	//		})
	//	}
	//}

	// Process input files and add them as blocks
	for _, inputPath := range input {
		// Validate file exists
		if _, err := os.Stat(inputPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("input file does not exist: %s", inputPath)
		}

		// Detect input type (text, image, etc.)
		modality, err := agent.DetectInputType(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to detect input type for %s: %w", inputPath, err)
		}

		// Read file content
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read input file %s: %w", inputPath, err)
		}

		// Apply size limits to prevent memory issues
		if len(content) > 50*1024*1024 { // 50MB limit
			return nil, fmt.Errorf("input file %s exceeds maximum size limit (50MB)", inputPath)
		}

		mime := mimetype.Detect(content).String()

		// Create block based on modality
		var block gai.Block
		switch modality {
		case gai.Text:
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(string(content)),
			}
		case gai.Image, gai.Video, gai.Audio:
			// For non-text files, encode as base64
			contentStr := base64.StdEncoding.EncodeToString(content)
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: modality,
				MimeType:     mime,
				Content:      gai.Str(contentStr),
			}
		default:
			return nil, fmt.Errorf("unsupported input type for %s", inputPath)
		}

		userBlocks = append(userBlocks, block)
	}

	// Add positional arguments if provided
	if len(args) > 0 {
		if len(args) != 1 {
			return nil, fmt.Errorf("too many arguments to process")
		}

		userBlocks = append(userBlocks, gai.Block{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str(args[0]),
		})
	}

	return userBlocks, nil
}

// printMessage prints a message to stdout
func printMessage(msg gai.Message) {
	for _, block := range msg.Blocks {
		if block.ModalityType == gai.Text {
			fmt.Println(block.Content.String())
		} else {
			fmt.Printf("[%s content of type %s]\n", block.BlockType, block.MimeType)
		}
	}
}

// getCustomURL returns the custom URL to use based on the following precedence:
// 1. Command-line flag (--custom-url)
// 2. General custom URL environment variable (CPE_CUSTOM_URL)
func getCustomURL(flagURL string) string {
	// Start with the flag value
	urlVal := flagURL

	// Finally, check the general custom URL env var
	if envURL := os.Getenv("CPE_CUSTOM_URL"); urlVal == "" && envURL != "" {
		urlVal = envURL
	}

	return urlVal
}
