package cmd

import (
	"fmt"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/tokentree"
	"github.com/spf13/cobra"
	"os"
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
	Long:  `Print a tree of directories and files with their token counts for the given path.`,
	Args:  cobra.MaximumNArgs(1),
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

		// Use current directory if no path is provided
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		// Change to the specified directory if needed
		if path != "." {
			currentDir, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
				os.Exit(1)
			}
			if err := os.Chdir(path); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to change directory to %s: %v\n", path, err)
				os.Exit(1)
			}
			defer os.Chdir(currentDir) // Change back to original directory when done
		}

		if err := tokentree.PrintTokenTree(os.DirFS("."), ignorer); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(toolsCmd)

	// Add subcommands to tools command
	toolsCmd.AddCommand(listFilesCmd)
	toolsCmd.AddCommand(tokenCountCmd)
}
