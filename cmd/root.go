package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/gabriel-vasile/mimetype"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

var (
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
	incognitoMode     bool
	systemPromptPath  string
	timeout           string

	// DefaultModel holds the global default LLM model for the CLI.
	// It is set at process startup from CPE_MODEL env var (or empty if unset).
	DefaultModel = os.Getenv("CPE_MODEL")
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
		return executeRootCommand(cmd.Context(), args)
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
	rootCmd.PersistentFlags().BoolVarP(&incognitoMode, "incognito", "G", false, "Run in incognito mode (do not save conversations to storage)")
	rootCmd.PersistentFlags().StringVarP(&systemPromptPath, "system-prompt-file", "s", "", "Specify a custom system prompt template file")
	rootCmd.PersistentFlags().StringVarP(&timeout, "timeout", "", "5m", "Specify request timeout duration (e.g. '5m', '30s')")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}

// executeRootCommand handles the main functionality of the root command
func executeRootCommand(ctx context.Context, args []string) error {
	if incognitoMode {
		fmt.Fprintln(os.Stderr, "WARNING: Incognito mode is enabled. This conversation will NOT be saved to storage.")
	}

	// Parse timeout duration
	requestTimeout, err := time.ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout value '%s': %w", timeout, err)
	}

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

	// Initialize or open the database through the storage package (for reading/threading)
	dialogStorage, err := storage.InitDialogStorage(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize dialog storage: %w", err)
	}
	defer dialogStorage.Close()

	// Get most recent message
	if continueID == "" && !newConversation {
		continueID, err = dialogStorage.GetMostRecentUserMessageId(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if !strings.Contains(err.Error(), "no rows in result set") {
				return fmt.Errorf("failed to get most recent message: %w", err)
			}
			newConversation = true
		}
	}

	// Use DefaultModel from global scope (must be set by env or CLI flag only)
	if model == "" {
		if DefaultModel == "" {
			return errors.New("No model specified. Please set the CPE_MODEL environment variable or use the --model flag.")
		}
		model = DefaultModel
	}

	customURL = getCustomURL(customURL)
	// Create the underlying generator based on the model name
	baseGenerator, err := agent.InitGenerator(model, customURL, systemPromptPath, requestTimeout)
	if err != nil {
		return fmt.Errorf("failed to create base generator: %w", err)
	}

	// Wrap the base generator with ResponsePrinterGenerator to print responses
	printingGenerator := agent.NewResponsePrinterGenerator(baseGenerator)

	// Create the tool generator using the wrapped generator
	toolGen := &gai.ToolGenerator{
		G: printingGenerator,
	}

	// Wrap the tool generator with ThinkingFilterToolGenerator to filter thinking blocks
	// only from the initial dialog, but preserve them during tool execution
	filterToolGen := agent.NewThinkingFilterToolGenerator(toolGen)

	// Register all the necessary tools
	if err = agent.RegisterTools(filterToolGen); err != nil {
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

	var msgIdList []string

	if !newConversation {
		dialog, msgIdList, err = dialogStorage.GetDialogForMessage(ctx, continueID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
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
		if thinkingBudget != "" {
			opts.ThinkingBudget = thinkingBudget
		}
		return opts
	}

	// The ResponsePrinterGenerator will print the responses as they come
	resultDialog, err := filterToolGen.Generate(ctx, dialog, genOptionsFunc)
	interrupted := errors.Is(err, context.Canceled)
	if err != nil && !interrupted {
		fmt.Fprintf(os.Stderr, "Error generating response: %v\n", err)
		os.Exit(1)
	}

	if incognitoMode {
		// Don't save any conversation messages in incognito mode!
		return nil
	}

	var parentId string
	if len(msgIdList) != 0 {
		parentId = msgIdList[len(msgIdList)-1]
	}

	// If we were interrupted, make sure to save any partial dialog returned,
	// but we also want to allow user to cancel storage operations by interrupting a second time
	dialogCtx := ctx
	if interrupted {
		fmt.Fprintln(os.Stderr, "\nWARNING: Generation was interrupted. Saving partial dialog. You can cancel this operation by interrupting again.")
		var cancel context.CancelFunc
		dialogCtx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
	}

	// Save user message
	userMsgID, err := dialogStorage.SaveMessage(dialogCtx, userMessage, parentId, "")
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Save assistant messages
	assistantMsgs := resultDialog[len(dialog):]

	parentId = userMsgID
	for _, assistantMsg := range assistantMsgs {
		parentId, err = dialogStorage.SaveMessage(dialogCtx, assistantMsg, parentId, "")
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("failed to save message: %w", err)
		}
	}

	return nil
}

// processUserInput processes and combines user input from all available sources
func processUserInput(args []string) ([]gai.Block, error) {
	var userBlocks []gai.Block

	// Get input from stdin if available (non-blocking)
	stdinStat, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to check stdin: %w", err)
	}

	// If stdin has data, read it
	// We check if SKIP_STDIN env var is set to skip reading from
	// stdin, as reading from stdin can hang in environments like
	// running in an IDE with a debugger. SKIP_STDIN is a hidden
	// env var and should not be used by the end user, just the authors
	if (stdinStat.Mode()&os.ModeCharDevice) == 0 && os.Getenv("SKIP_STDIN") != "" {
		fmt.Println("SKIPPING READING FROM STDIN")
	}
	if (stdinStat.Mode()&os.ModeCharDevice) == 0 && os.Getenv("SKIP_STDIN") == "" {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		if len(stdinBytes) > 0 {
			userBlocks = append(userBlocks, gai.Block{
				BlockType:    "text",
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(stdinBytes),
			})
		}
	}

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
