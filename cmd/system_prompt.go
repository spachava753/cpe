package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spf13/cobra"
)

var systemPromptCmd = &cobra.Command{
	Use:     "system-prompt",
	Short:   "Show the rendered system prompt",
	Long:    `Render and display the system prompt template to help debug template issues.`,
	Aliases: []string{"sp", "prompt"},
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no system prompt path is provided, show usage
		if systemPromptPath == "" {
			fmt.Println(`No system prompt template file specified.
Use -s/--system-prompt-file to specify a template file.

Example:
  cpe system-prompt -s my-template.txt`)
			return nil
		}

		// Check if the file exists
		if _, err := os.Stat(systemPromptPath); os.IsNotExist(err) {
			return fmt.Errorf("system prompt template file not found: %s", systemPromptPath)
		}

		// Prepare the system prompt (this will execute the template)
		systemPrompt, err := agent.PrepareSystemPrompt(systemPromptPath)
		if err != nil {
			return fmt.Errorf("failed to render system prompt template: %w", err)
		}

		// Display the rendered prompt
		fmt.Println(systemPrompt)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(systemPromptCmd)
}
