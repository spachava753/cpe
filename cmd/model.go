package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/agent"
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
			line := model.Ref
			if defaultModel != "" && model.Ref == defaultModel {
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

		fmt.Printf("Ref: %s\nDisplay Name: %s\nType: %s\nID: %s\nContext: %d\nMaxOutput: %d\nInputCostPerMillion: %.6f\nOutputCostPerMillion: %.6f\n",
			model.Ref, model.DisplayName, model.Type, model.ID, model.ContextWindow, model.MaxOutput, model.InputCostPerMillion, model.OutputCostPerMillion,
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

var systemPromptModelCmd = &cobra.Command{
	Use:   "system-prompt",
	Short: "Show the rendered system prompt for a model",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		modelName := model
		if modelName == "" {
			if cfg.GetDefaultModel() != "" {
				modelName = cfg.GetDefaultModel()
			} else if DefaultModel != "" {
				modelName = DefaultModel
			}
		}

		if modelName == "" {
			return fmt.Errorf("no model specified. Use --model flag or set defaults.model in configuration")
		}

		selectedModel, found := cfg.FindModel(modelName)
		if !found {
			return fmt.Errorf("model %q not found in configuration", modelName)
		}

		effectiveSystemPromptPath := selectedModel.GetEffectiveSystemPromptPath(
			cfg.Defaults.SystemPromptPath,
			systemPromptPath,
		)

		if effectiveSystemPromptPath == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Model %q does not define a system prompt.\n", modelName)
			return nil
		}

		rendered, err := agent.PrepareSystemPrompt(effectiveSystemPromptPath, &selectedModel.Model)
		if err != nil {
			return fmt.Errorf("failed to render system prompt: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Model: %s\nPath: %s\n\n%s\n", modelName, effectiveSystemPromptPath, rendered)
		return nil
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	modelCmd.AddCommand(systemPromptModelCmd)
	rootCmd.AddCommand(modelCmd)
}
