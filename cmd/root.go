package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
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
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/urlhandler"
	"github.com/spachava753/cpe/internal/version"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

// DefaultModel holds the global default LLM model for the CLI.
// It is set at process startup from CPE_MODEL env var (or empty if unset).
var DefaultModel = os.Getenv("CPE_MODEL")

var (
	model            string
	customURL        string
	input            []string
	newConversation  bool
	continueID       string
	incognitoMode    bool
	timeout          string
	disableStreaming bool
	skipStdin        bool
	configPath       string

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
	rootCmd.PersistentFlags().BoolVar(&disableStreaming, "no-stream", false, "Disable streaming output (show complete response after generation)")
	rootCmd.PersistentFlags().BoolVar(&skipStdin, "skip-stdin", false, "Skip reading from stdin (useful in scripts)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to unified configuration file (default: ./cpe.yaml, ~/.config/cpe/cpe.yaml)")

	// Add version flag
	rootCmd.Flags().BoolP("version", "v", false, "Print the version number and exit")
}

// executeRootCommand handles the main functionality of the root command
func executeRootCommand(ctx context.Context, args []string) error {
	userBlocks, err := processUserInput(args)
	if err != nil {
		return fmt.Errorf("could not process user input: %w", err)
	}

	// Resolve effective config with runtime options
	noStream := disableStreaming
	effectiveConfig, err := config.ResolveConfig(configPath, config.RuntimeOptions{
		ModelRef:  model,
		GenParams: &genParams,
		Timeout:   timeout,
		NoStream:  &noStream,
	})
	if err != nil {
		return fmt.Errorf("failed to resolve configuration: %w", err)
	}

	// Load and render system prompt
	var systemPrompt string
	if effectiveConfig.SystemPromptPath != "" {
		f, err := os.Open(effectiveConfig.SystemPromptPath)
		if err != nil {
			return fmt.Errorf("could not open system prompt file: %w", err)
		}
		defer f.Close()

		contents, err := io.ReadAll(f)
		if err != nil {
			return fmt.Errorf("failed to read system prompt file: %w", err)
		}

		systemPrompt, err = agent.SystemPromptTemplate(string(contents), agent.TemplateData{
			Config: effectiveConfig,
		})
		if err != nil {
			return fmt.Errorf("failed to prepare system prompt: %w", err)
		}
	}

	if customURL == "" {
		customURL = effectiveConfig.Model.BaseUrl
	}

	// Create the generator
	toolGen, err := agent.CreateToolCapableGenerator(
		ctx,
		effectiveConfig.Model,
		systemPrompt,
		effectiveConfig.Timeout,
		customURL,
		effectiveConfig.NoStream,
		effectiveConfig.MCPServers,
	)
	if err != nil {
		return fmt.Errorf("failed to create tool capable generator: %w", err)
	}

	// Initialize storage unless in incognito mode
	var dialogStorage commands.DialogStorage
	if !incognitoMode {
		dbPath := ".cpeconvo"
		dialogStorage, err = storage.InitDialogStorage(dbPath)
		if err != nil {
			return fmt.Errorf("failed to initialize dialog storage: %w", err)
		}
		defer dialogStorage.Close()
	}

	genOpts := effectiveConfig.GenerationDefaults

	// Call the business logic
	return commands.Generate(ctx, commands.GenerateOptions{
		UserBlocks:      userBlocks,
		ContinueID:      continueID,
		NewConversation: newConversation,
		IncognitoMode:   incognitoMode,
		GenOptsFunc: func(dialog gai.Dialog) *gai.GenOpts {
			return genOpts
		},
		Storage:   dialogStorage,
		Generator: toolGen,
		Stderr:    os.Stderr,
	})
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
