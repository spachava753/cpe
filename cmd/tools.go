package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/mcp"
	tokenbuilder "github.com/spachava753/cpe/internal/token/builder"
	tokentree "github.com/spachava753/cpe/internal/token/tree"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

// toolsCmd represents the tools command
var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Access various utility tools",
	Long:  `Access various utility tools for file overview, related files, and token counting.`,
}

// listFilesCmd represents the list-files subcommand
var listFilesCmd = &cobra.Command{
	Use:   "list-files",
	Short: "List all text files",
	Long:  `List all text files in the current directory recursively.`,
	Run: func(cmd *cobra.Command, args []string) {
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

		files, err := mcp.ListTextFiles(ignorer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, file := range files {
			fmt.Printf("File: %s\nContent:\n%s\n\n", file.Path, file.Content)
		}
	},
}

// tokenCountCmd represents the token-count subcommand
var tokenCountCmd = &cobra.Command{
	Use:   "token-count [path]",
	Short: "Count tokens in files",
	Long: `Count tokens in a file, directory, or stdin content using a model-specific tokenizer.\n
If no path is provided, the current directory is used.\nIf path is "-", content is read from stdin.\nIf no model is specified, the default model (CPE_MODEL) is used.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate flags
		if tokentree.MaxConcurrentFiles <= 0 {
			return fmt.Errorf("concurrency must be a positive integer, got %d", tokentree.MaxConcurrentFiles)
		}

		if tokentree.MaxFileSize <= 0 {
			return fmt.Errorf("max-file-size must be a positive integer, got %d", tokentree.MaxFileSize)
		}

		// Initialize ignorer for skipping files
		ignorer, err := ignore.LoadIgnoreFiles(".")
		if err != nil {
			return fmt.Errorf("failed to load ignore files: %w", err)
		}
		if ignorer == nil {
			return fmt.Errorf("git ignorer was nil")
		}

		// Get model name using the persistent flag from rootCmd
		// The model variable is defined in cmd/root.go
		if model == "" { // model is from root.go
			if DefaultModel == "" {
				return fmt.Errorf("no model specified and no default model (CPE_MODEL) set")
			}
			model = DefaultModel // Use default model if not specified
			fmt.Fprintf(os.Stderr, "Using default model: %s\n", model)
		}

		// Initialize generator for token counting
		// timeout is also from root.go
		requestTimeout, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("failed to parse request timeout: %w", err)
		}

		// For token counting, we use an empty system prompt to get accurate counts
		gen, err := agent.InitGenerator(model, "", "", requestTimeout)
		if err != nil {
			return fmt.Errorf("failed to initialize generator for token counting: %w", err)
		}

		// Get token counter from generator
		tokenCounter, ok := gen.(gai.TokenCounter)
		if !ok {
			return fmt.Errorf("the selected model doesn't support token counting")
		}

		// Determine the path to count tokens for
		var path string
		if len(args) == 0 {
			path = "."
		} else {
			path = args[0]
		}

		// Handle stdin case
		if path == "-" {
			content, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}

			count, err := tokenbuilder.CountStdin(cmd.Context(), content, tokenCounter)
			if err != nil {
				return fmt.Errorf("failed to count tokens from stdin: %w", err)
			}

			fmt.Printf("Token count: %d\n", count)
			return nil
		}

		// Handle file/directory case
		fileInfo, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("failed to access path %s: %w", path, err)
		}

		if fileInfo.IsDir() {
			// Build the directory tree with token counts, always showing progress
			tree, err := tokentree.BuildDirTree(
				cmd.Context(),
				path,
				ignorer,
				tokenCounter,
				os.Stderr, // Progress writer
			)
			if err != nil {
				return fmt.Errorf("failed to count tokens in directory: %w", err)
			}

			// Print the directory tree with token counts
			tokentree.PrintDirTree(tree, "")
			return nil
		} else {
			// Count tokens in a single file
			count, err := tokenbuilder.CountFile(cmd.Context(), path, tokenCounter)
			if err != nil {
				return fmt.Errorf("failed to count tokens in file: %w", err)
			}

			fmt.Printf("Token count for file %s: %d\n", path, count)
			return nil
		}
	},
}

func init() {
	rootCmd.AddCommand(toolsCmd)

	// Add subcommands to tools command
	toolsCmd.AddCommand(listFilesCmd)
	toolsCmd.AddCommand(tokenCountCmd)

	// Add flags to token count command
	tokenCountCmd.Flags().IntVar(&tokentree.MaxConcurrentFiles, "concurrency", tokentree.DefaultMaxConcurrentFiles, "Maximum number of files to process concurrently")
	tokenCountCmd.Flags().IntVar(&tokentree.MaxFileSize, "max-file-size", tokentree.DefaultMaxFileSize, "Maximum file size to process in bytes")
}
