package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spf13/cobra"
)

// systemPromptRenderer implements SystemPromptRenderer using agent templates
type systemPromptRenderer struct{}

func (r *systemPromptRenderer) Render(template string, model *config.Model) (string, error) {
	sysInfo, err := agent.GetSystemInfoWithModel(model)
	if err != nil {
		return "", fmt.Errorf("failed to get system info: %w", err)
	}
	return sysInfo.ExecuteTemplateString(template)
}

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

		// Find the model
		selectedModel, found := cfg.FindModel(modelName)
		if !found {
			return fmt.Errorf("model %q not found in configuration", modelName)
		}

		// Determine effective system prompt path
		effectiveSystemPromptPath := selectedModel.GetEffectiveSystemPromptPath(
			cfg.Defaults.SystemPromptPath,
			systemPromptPath,
		)

		// Read the template file
		var templateContent string
		if effectiveSystemPromptPath != "" {
			content, err := os.ReadFile(effectiveSystemPromptPath)
			if err != nil {
				return fmt.Errorf("failed to read system prompt file: %w", err)
			}
			templateContent = string(content)
		}

		return commands.ModelSystemPrompt(cmd.Context(), commands.ModelSystemPromptOptions{
			Config:               cfg,
			ModelName:            modelName,
			SystemPromptTemplate: templateContent,
			SystemPromptPath:     effectiveSystemPromptPath,
			Writer:               cmd.OutOrStdout(),
			SystemPromptRenderer: &systemPromptRenderer{},
		})
	},
}

func init() {
	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	modelCmd.AddCommand(systemPromptModelCmd)
	rootCmd.AddCommand(modelCmd)
}
