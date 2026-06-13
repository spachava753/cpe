package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
)

// modelCmd is the CLI group for inspecting model profiles from cpe.yaml.
var modelCmd = &cobra.Command{
	Use:     "model",
	Short:   "Inspect configured model profiles",
	Long:    `List configured model profiles and inspect the settings CPE exposes to ACP sessions.`,
	Aliases: []string{"models"},
}

// listModelCmd resolves raw config and delegates formatted listing to
// commands.ModelList.
var listModelCmd = &cobra.Command{
	Use:     "list",
	Short:   "List models from configuration",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.ModelListFromConfig(cmd.Context(), commands.ModelListFromConfigOptions{
			ConfigPath:   configPath,
			DefaultModel: model,
			Writer:       cmd.OutOrStdout(),
		})
	},
}

// infoModelCmd resolves raw config and delegates model detail rendering to
// commands.ModelInfo.
var infoModelCmd = &cobra.Command{
	Use:   "info <model-name>",
	Short: "Show model details by name",
	Example: `# Show model details by name
cpe model info sonnet
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name == "" {
			name = os.Getenv("CPE_MODEL")
		}

		return commands.ModelInfoFromConfig(cmd.Context(), commands.ModelInfoFromConfigOptions{
			ConfigPath: configPath,
			ModelName:  name,
			Writer:     cmd.OutOrStdout(),
		})
	},
}

// systemPromptModelCmd resolves raw config and delegates system prompt
// selection/rendering rules to commands.ModelSystemPrompt.
var systemPromptModelCmd = &cobra.Command{
	Use:   "system-prompt",
	Short: "Show the rendered system prompt for a model",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.ModelSystemPromptFromConfig(cmd.Context(), commands.ModelSystemPromptFromConfigOptions{
			ConfigPath:   configPath,
			ModelName:    model,
			DefaultModel: DefaultModel,
			Output:       cmd.OutOrStdout(),
			Stderr:       cmd.ErrOrStderr(),
		})
	},
}

func init() {
	modelCmd.PersistentFlags().StringVarP(&model, "model", "m", DefaultModel, "Model profile ref for profile-specific inspection (env: CPE_MODEL)")

	modelCmd.AddCommand(listModelCmd)
	modelCmd.AddCommand(infoModelCmd)
	modelCmd.AddCommand(systemPromptModelCmd)
	rootCmd.AddCommand(modelCmd)
}
