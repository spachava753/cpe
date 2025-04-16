package cmd

import (
	"fmt"
	"github.com/spachava753/cpe/internal/agent"
	"github.com/spf13/cobra"
)

// modelCmd represents the model management command group
var modelCmd = &cobra.Command{
	Use:     "model",
	Short:   "Manage LLM models",
	Long:    `Show and interact with models supported by CPE.`,
	Aliases: []string{"models"},
}

// listModelCmd represents the 'model list' and 'model ls' subcommand
var listModelCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all known models",
	Long:    `List all tracked model names available to CPE (Anthropic, OpenAI, etc).`,
	Aliases: []string{"ls"},
	Run: func(cmd *cobra.Command, args []string) {
		for _, model := range agent.KnownModels {
			fmt.Println(model)
		}
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	rootCmd.AddCommand(modelCmd)
}
