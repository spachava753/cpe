package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CPE configuration",
}

var configAddRef string

var configAddCmd = &cobra.Command{
	Use:   "add <provider>/<model-id>",
	Short: "Add a model from the models.dev registry to your configuration",
	Long: `Add a model from the models.dev registry to your CPE configuration.

The command fetches model information from https://models.dev/api.json and adds
the model to your configuration file with appropriate defaults.

Examples:
  cpe config add anthropic/claude-sonnet-4-20250514
  cpe config add openai/gpt-4o
  cpe config add google/gemini-2.5-pro
  cpe config add anthropic/claude-sonnet-4-20250514 --ref sonnet`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.ConfigAdd(cmd.Context(), commands.ConfigAddOptions{
			ModelSpec:  args[0],
			ConfigPath: configPath,
			Ref:        configAddRef,
			Writer:     os.Stdout,
		})
	},
}

var configRemoveCmd = &cobra.Command{
	Use:   "remove <ref>",
	Short: "Remove a model from your configuration by ref",
	Long:  "Remove a model from your CPE configuration file by its reference name.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.ConfigRemove(cmd.Context(), commands.ConfigRemoveOptions{
			Ref:        args[0],
			ConfigPath: configPath,
			Writer:     os.Stdout,
		})
	},
}

func init() {
	configAddCmd.Flags().StringVar(&configAddRef, "ref", "", "Custom reference name for the model (defaults to model ID)")
	configCmd.AddCommand(configAddCmd)
	configCmd.AddCommand(configRemoveCmd)
	rootCmd.AddCommand(configCmd)
}
