package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/version"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gabriel-vasile/mimetype"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/urlhandler"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

// DefaultModel holds the global default LLM model for the CLI.
// It is set at process startup from CPE_MODEL env var (or empty if unset).
var DefaultModel = os.Getenv("CPE_MODEL")

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
	disableStreaming  bool
	mcpConfigPath     string
	skipStdin         bool
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

func init() {
	// Define flags for the root command
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", DefaultModel, "Specify the model to use")
	rootCmd.PersistentFlags().StringVar(&customURL, "custom-url", "", "Specify a custom base URL for the model provider API")
	rootCmd.PersistentFlags().IntVarP(&maxTokens, "max-tokens", "x", 0, "Maximum number of tokens to generate")
	rootCmd.PersistentFlags().Float64VarP(&temperature, "temperature", "t", 0, "Sampling temperature (0.0 - 1.0)")
	rootCmd.PersistentFlags().Float64Var(&topP, "top-p", 0, "Nucleus sampling parameter (0.0 - 1.0)")
	rootCmd.PersistentFlags().IntVar(&topK, "top-k", 0, "Top-k sampling parameter")
	rootCmd.PersistentFlags().Float64Var(&frequencyPenalty, "frequency-penalty", 0, "Frequency penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().Float64Var(&presencePenalty, "presence-penalty", 0, "Presence penalty (-2.0 - 2.0)")
	rootCmd.PersistentFlags().IntVar(&numberOfResponses, "number-of-responses", 0, "Number of responses to generate")
	rootCmd.PersistentFlags().StringVarP(&thinkingBudget, "thinking-budget", "b", "", "Budget for reasoning/thinking capabilities (string or numerical value)")
	rootCmd.PersistentFlags().StringSliceVarP(&input, "input", "i", []string{}, "Specify input files or HTTP(S) URLs to process. Multiple inputs can be provided.")
	rootCmd.PersistentFlags().BoolVarP(&newConversation, "new", "n", false, "Start a new conversation instead of continuing from the last one")
	rootCmd.PersistentFlags().StringVarP(&continueID, "continue", "c", "", "Continue from a specific conversation ID")
	rootCmd.PersistentFlags().BoolVarP(&incognitoMode, "incognito", "G", false, "Run in incognito mode (do not save conversations to storage)")
	rootCmd.PersistentFlags().StringVarP(&systemPromptPath, "system-prompt-file", "s", "", "Specify a custom system prompt template file")
	rootCmd.PersistentFlags().StringVarP(&timeout, "timeout", "", "5m", "Specify request timeout duration (e.g. '5m', '30s')")
	rootCmd.PersistentFlags().BoolVar(&disableStreaming, "no-stream", false, "Disable streaming output (show complete response after generation)")
	rootCmd.PersistentFlags().StringVar(&mcpConfigPath, "mcp-config", "", "Specify path to MCP configuration file")
	rootCmd.PersistentFlags().BoolVar(&skipStdin, "skip-stdin", false, "Skip reading from stdin (useful in scripts)")

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
		return errors.New("no model specified. Please set the CPE_MODEL environment variable or use the --model flag")
	}

	customURL = getCustomURL(customURL)

	// Prepare system prompt
	systemPrompt, err := agent.PrepareSystemPrompt(systemPromptPath)
	if err != nil {
		return fmt.Errorf("failed to prepare system prompt: %w", err)
	}

	// Create the generator
	baseGenerator, err := agent.InitGenerator(model, customURL, systemPrompt, requestTimeout)
	if err != nil {
		return fmt.Errorf("failed to create generator: %w", err)
	}

	// Check if the generator supports streaming and if streaming is enabled
	var gen gai.ToolCapableGenerator
	if streamingGen, ok := baseGenerator.(gai.StreamingGenerator); ok && !disableStreaming {
		// Wrap with streaming printer
		streamingPrinter := agent.NewStreamingPrinterGenerator(streamingGen)
		// Use StreamingAdapter to convert back to Generator
		adapter := &gai.StreamingAdapter{S: streamingPrinter}
		gen = any(adapter).(gai.ToolCapableGenerator)
	} else {
		// Use ResponsePrinterGenerator for non-streaming generators or when streaming is disabled
		gen = agent.NewResponsePrinterGenerator(baseGenerator.(gai.ToolCapableGenerator))
	}

	// Create the tool generator using the printing-enabled generator
	toolGen := &gai.ToolGenerator{
		G: gen,
	}

	// Wrap the tool generator with ThinkingFilterToolGenerator to filter thinking blocks
	// only from the initial dialog, but preserve them during tool execution
	filterToolGen := agent.NewThinkingFilterToolGenerator(toolGen)

	// Load MCP configuration
	config, err := mcp.LoadConfig(mcpConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load MCP configuration: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid MCP configuration: %w", err)
	}

	// Create client manager
	clientManager := mcp.NewClientManager(config)
	defer clientManager.Close()

	// Register MCP server tools
	if err = mcp.RegisterMCPServerTools(ctx, clientManager, filterToolGen); err != nil {
		return fmt.Errorf("failed to register MCP tools: %v\n", err)
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

	// Generate the response
	resultDialog, err := filterToolGen.Generate(ctx, dialog, genOptionsFunc)
	interrupted := errors.Is(err, context.Canceled)
	// If we were not interrupted, print the error message, but continue to saving the returned dialog
	if err != nil && !interrupted {
		fmt.Fprintf(os.Stderr, "Error generating response: %v\n", err)
	}

	if incognitoMode {
		// Don't save any conversation messages in incognito mode!
		return nil
	}

	var parentId string
	if len(msgIdList) != 0 {
		parentId = msgIdList[len(msgIdList)-1]
	}

	// If we were interrupted, prepare a new context for the save operation
	// that can also be cancelled.
	dialogCtx := ctx
	var saveCancel context.CancelFunc
	if interrupted {
		fmt.Fprintln(os.Stderr, "\nWARNING: Generation was interrupted. Attempting to save partial dialog.")
	}
	fmt.Fprintln(os.Stderr, "You can cancel this save operation by interrupting (Ctrl+C).")
	dialogCtx, saveCancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer saveCancel() // Ensure this new context's cancel is called

	// Determine assistant messages from the result
	// resultDialog contains the original dialog + any new assistant messages
	// dialog contains the original dialog sent to the model
	assistantMsgs := resultDialog[len(dialog):]

	shouldSave := len(assistantMsgs) > 0

	if !shouldSave && interrupted {
		fmt.Fprintln(os.Stderr, "No new assistant messages to save from interrupted generation. Skipping save for this turn.")
		return nil // Do not save the user message if no assistant messages were generated during interruption
	}

	// Save user message (part of the current turn)
	// This userMessage is the one that initiated the current turn.
	userMsgID, err := dialogStorage.SaveMessage(dialogCtx, userMessage, parentId, "")
	if err != nil {
		if errors.Is(err, context.Canceled) { // User cancelled the save operation itself
			fmt.Fprintln(os.Stderr, "Save operation cancelled by user.")
			return nil
		}
		return fmt.Errorf("failed to save user message: %w", err)
	}

	// Save assistant messages (if any)
	currentParentId := userMsgID
	for _, assistantMsg := range assistantMsgs {
		currentParentId, err = dialogStorage.SaveMessage(dialogCtx, assistantMsg, currentParentId, "")
		if err != nil {
			if errors.Is(err, context.Canceled) { // User cancelled the save operation during assistant message saving
				fmt.Fprintln(os.Stderr, "Save operation cancelled by user during assistant message saving.")
				return nil
			}
			return fmt.Errorf("failed to save assistant message: %w", err)
		}
	}

	if interrupted && len(assistantMsgs) > 0 {
		fmt.Fprintln(os.Stderr, "Partial dialog saved successfully.")
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
	if (stdinStat.Mode()&os.ModeCharDevice) == 0 && !skipStdin {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		if len(stdinBytes) > 0 {
			userBlocks = append(userBlocks, gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(stdinBytes),
			})
		}
	}

	// Process input files and URLs and add them as blocks
	for _, inputPath := range input {
		var content []byte
		var filename string
		var contentType string

		// Check if input is a URL or file path
		if urlhandler.IsURL(inputPath) {
			// Handle URL input
			fmt.Fprintf(os.Stderr, "Downloading: %s\n", inputPath)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			downloaded, err := urlhandler.DownloadContent(ctx, inputPath, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to download content from URL %s: %w", inputPath, err)
			}

			content = downloaded.Data
			filename = filepath.Base(inputPath) // Extract filename from URL path
			contentType = downloaded.ContentType

			fmt.Fprintf(os.Stderr, "Downloaded %d bytes from %s\n", len(content), inputPath)
		} else {
			// Handle local file input
			// Validate file exists
			if _, err := os.Stat(inputPath); os.IsNotExist(err) {
				return nil, fmt.Errorf("input file does not exist: %s", inputPath)
			}

			// Read file content
			var err error
			content, err = os.ReadFile(inputPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read input file %s: %w", inputPath, err)
			}

			filename = filepath.Base(inputPath)
		}

		// Apply size limits to prevent memory issues
		if len(content) > 50*1024*1024 { // 50MB limit
			return nil, fmt.Errorf("input content from %s exceeds maximum size limit (50MB)", inputPath)
		}

		// Detect input type (text, image, etc.)
		modality, err := agent.DetectInputType(content)
		if err != nil {
			return nil, fmt.Errorf("failed to detect input type for %s: %w", inputPath, err)
		}

		// Determine MIME type
		var mime string
		if contentType != "" {
			// Use Content-Type from HTTP response if available
			mime = strings.Split(contentType, ";")[0] // Remove charset and other parameters
		} else {
			// Fall back to content-based detection
			mime = mimetype.Detect(content).String()
		}

		// Create block based on modality
		var block gai.Block
		switch modality {
		case gai.Text:
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(content),
			}
		case gai.Video:
			contentStr := base64.StdEncoding.EncodeToString(content)
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: modality,
				MimeType:     mime,
				Content:      gai.Str(contentStr),
			}
		case gai.Audio:
			block = gai.AudioBlock(content, mime)
		case gai.Image:
			if mime == "application/pdf" {
				block = gai.PDFBlock(content, filename)
			} else {
				block = gai.ImageBlock(content, mime)
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
