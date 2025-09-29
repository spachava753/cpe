package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	tokenbuilder "github.com/spachava753/cpe/internal/token/builder"
	tokentree "github.com/spachava753/cpe/internal/token/tree"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

// tokenCountCmd represents the token-count subcommand
var tokenCountCmd = &cobra.Command{
	Use:   "token-count [path]",
	Short: "Count tokens in files",
	Long: `Count tokens in a file, directory, or stdin content using a model-specific tokenizer.\n
If no path is provided, the current directory is used.\nIf path is "-", content is read from stdin.\nIf no model is specified, the default model (CPE_MODEL) is used.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if tokentree.MaxConcurrentFiles <= 0 {
			return fmt.Errorf("concurrency must be a positive integer, got %d", tokentree.MaxConcurrentFiles)
		}
		if tokentree.MaxFileSize <= 0 {
			return fmt.Errorf("max-file-size must be a positive integer, got %d", tokentree.MaxFileSize)
		}

		if model == "" {
			if DefaultModel == "" {
				return fmt.Errorf("no model specified and no default model (CPE_MODEL) set")
			}
			model = DefaultModel
			fmt.Fprintf(os.Stderr, "Using default model: %s\n", model)
		}

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		selectedModel, found := cfg.FindModel(model)
		if !found {
			return fmt.Errorf("model %q not found in configuration", model)
		}

		requestTimeout, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("failed to parse request timeout: %w", err)
		}

		gen, err := agent.InitGeneratorFromModel(selectedModel.Model, "", requestTimeout, "")
		if err != nil {
			return fmt.Errorf("failed to initialize generator for token counting: %w", err)
		}

		tokenCounter, ok := gen.(gai.TokenCounter)
		if !ok {
			return fmt.Errorf("the selected model doesn't support token counting")
		}

		var path string
		if len(args) == 0 {
			path = "."
		} else {
			path = args[0]
		}

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

		fileInfo, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("failed to access path %s: %w", path, err)
		}
		if fileInfo.IsDir() {
			tree, err := tokentree.BuildDirTree(cmd.Context(), path, tokenCounter, os.Stderr)
			if err != nil {
				return fmt.Errorf("failed to count tokens in directory: %w", err)
			}
			tokentree.PrintDirTree(tree, "")
			return nil
		}
		count, err := tokenbuilder.CountFile(cmd.Context(), path, tokenCounter)
		if err != nil {
			return fmt.Errorf("failed to count tokens in file: %w", err)
		}
		fmt.Printf("Token count for file %s: %d\n", path, count)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tokenCountCmd)
	tokenCountCmd.Flags().IntVar(&tokentree.MaxConcurrentFiles, "concurrency", tokentree.DefaultMaxConcurrentFiles, "Maximum number of files to process concurrently")
	tokenCountCmd.Flags().IntVar(&tokentree.MaxFileSize, "max-file-size", tokentree.DefaultMaxFileSize, "Maximum file size to process in bytes")
}
