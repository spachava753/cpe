package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/tokentree"
	"github.com/spf13/cobra"
)

// toolsCmd represents the tools command
var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Access various utility tools",
	Long:  `Access various utility tools for file overview, related files, and token counting.`,
}

// overviewCmd represents the overview subcommand
var overviewCmd = &cobra.Command{
	Use:   "overview",
	Short: "Get an overview of all files",
	Long:  `Get an overview of all files in the current directory with reduced content.`,
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

		path := "."
		if len(args) > 0 && args[0] != "" {
			path = args[0]
		}
		result, err := agent.CreateExecuteFilesOverviewFunc(ignorer)(cmd.Context(), agent.FileOverviewInput{Path: path})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(result)
	},
}

// relatedFilesCmd represents the related-files subcommand
var relatedFilesCmd = &cobra.Command{
	Use:   "related-files [file1,file2,...]",
	Short: "Get related files",
	Long:  `Get related files for the given comma-separated list of files.`,
	Args:  cobra.ExactArgs(1),
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

		// Split the comma-separated list of files
		inputFiles := strings.Split(args[0], ",")
		// Trim whitespace from each file path
		for i := range inputFiles {
			inputFiles[i] = strings.TrimSpace(inputFiles[i])
		}

		result, err := agent.CreateExecuteGetRelatedFilesFunc(ignorer)(cmd.Context(), agent.GetRelatedFilesInput{InputFiles: inputFiles})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(result)
	},
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

		files, err := agent.ListTextFiles(ignorer)
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
	toolsCmd.AddCommand(overviewCmd)
	toolsCmd.AddCommand(relatedFilesCmd)
	toolsCmd.AddCommand(listFilesCmd)
	toolsCmd.AddCommand(tokenCountCmd)
}
