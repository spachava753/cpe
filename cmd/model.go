package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/commands"
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
		cfg, err := config.LoadRawConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		defaultModel := cfg.Defaults.Model
		if defaultModel == "" {
			defaultModel = DefaultModel
		}

		return commands.ModelList(cmd.Context(), commands.ModelListOptions{
			Config:       cfg,
			DefaultModel: defaultModel,
			Writer:       os.Stdout,
		})
	},
}

var infoModelCmd = &cobra.Command{
	Use:   "info",
	Short: "Show model details by name",
	Example: `# Show model details by name
cpe model info sonnet
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadRawConfig(configPath)
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

		return commands.ModelInfo(cmd.Context(), commands.ModelInfoOptions{
			Config:    cfg,
			ModelName: name,
			Writer:    os.Stdout,
		})
	},
}

var systemPromptModelCmd = &cobra.Command{
	Use:   "system-prompt",
	Short: "Show the rendered system prompt for a model",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadRawConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		modelName := model
		if modelName == "" {
			if cfg.Defaults.Model != "" {
				modelName = cfg.Defaults.Model
			} else if DefaultModel != "" {
				modelName = DefaultModel
			}
		}

		if modelName == "" {
			return fmt.Errorf("no model specified. Use --model flag or set defaults.model in configuration")
		}

		systemPromptPath := cfg.Defaults.SystemPromptPath
		if m, ok := cfg.FindModel(modelName); ok && m.SystemPromptPath != "" {
			systemPromptPath = m.SystemPromptPath
		}

		f, err := os.Open(systemPromptPath)
		if err != nil {
			return err
		}

		return commands.ModelSystemPrompt(commands.ModelSystemPromptOptions{
			Config:       cfg,
			ModelName:    modelName,
			Output:       cmd.OutOrStdout(),
			SystemPrompt: f,
		})
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	modelCmd.AddCommand(systemPromptModelCmd)
	rootCmd.AddCommand(modelCmd)
}
