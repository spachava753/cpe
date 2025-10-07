package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spf13/cobra"
)

var modelCmd = &cobra.Command{
	Use:     "model",
	Short:   "Manage LLM models",
	Long:    `Show and interact with models defined in a JSON catalog via --model-catalog.`,
	Aliases: []string{"models"},
}

var listModelCmd = &cobra.Command{
	Use:     "list",
	Short:   "List models from configuration",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		defaultModel := cfg.GetDefaultModel()
		if defaultModel == "" {
			defaultModel = DefaultModel
		}

		for _, model := range cfg.Models {
			line := model.Name
			if defaultModel != "" && model.Name == defaultModel {
				line += " (default)"
			}
			fmt.Println(line)
		}
		return nil
	},
}

var infoModelCmd = &cobra.Command{
	Use:   "info",
	Short: "Show model details by name",
	Example: `# Show model details by name
cpe model info sonnet
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		if len(args) != 1 {
			return cmd.Usage()
		}
		name := args[0]
		if name == "" {
			name = os.Getenv("CPE_MODEL")
		}
		if name == "" {
			return fmt.Errorf("no model name provided")
		}

		model, found := cfg.FindModel(name)
		if !found {
			return fmt.Errorf("model %q not found", name)
		}

		fmt.Printf("Name: %s\nType: %s\nID: %s\nContext: %d\nMaxOutput: %d\nInputCostPerMillion: %.6f\nOutputCostPerMillion: %.6f\n",
			model.Name, model.Type, model.ID, model.ContextWindow, model.MaxOutput, model.InputCostPerMillion, model.OutputCostPerMillion,
		)

		// Show generation defaults if present
		if model.GenerationDefaults != nil {
			fmt.Printf("\nGeneration Defaults:\n")
			if model.GenerationDefaults.Temperature != nil {
				fmt.Printf("  Temperature: %.2f\n", *model.GenerationDefaults.Temperature)
			}
			if model.GenerationDefaults.TopP != nil {
				fmt.Printf("  TopP: %.2f\n", *model.GenerationDefaults.TopP)
			}
			if model.GenerationDefaults.TopK != nil {
				fmt.Printf("  TopK: %d\n", *model.GenerationDefaults.TopK)
			}
			if model.GenerationDefaults.MaxTokens != nil {
				fmt.Printf("  MaxTokens: %d\n", *model.GenerationDefaults.MaxTokens)
			}
			if model.GenerationDefaults.ThinkingBudget != nil {
				fmt.Printf("  ThinkingBudget: %s\n", *model.GenerationDefaults.ThinkingBudget)
			}
		}

		return nil
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	rootCmd.AddCommand(modelCmd)
}
