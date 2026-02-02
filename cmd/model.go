package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
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
	Use:   "info <model-name>",
	Short: "Show model details by name",
	Example: `# Show model details by name
cpe model info sonnet
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadRawConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
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

		return commands.ModelSystemPrompt(cmd.Context(), commands.ModelSystemPromptOptions{
			Config:       cfg,
			ModelName:    model,
			DefaultModel: DefaultModel,
			Output:       cmd.OutOrStdout(),
		})
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	modelCmd.AddCommand(systemPromptModelCmd)
	rootCmd.AddCommand(modelCmd)
}
