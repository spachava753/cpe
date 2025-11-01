package cmd

import (
	"fmt"
	"os"

	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	Long:  `Manage and validate CPE configuration files.`,
}

var configLintCmd = &cobra.Command{
	Use:   "lint [config-file]",
	Short: "Validate CPE configuration file",
	Long: `Validate a CPE configuration file for correctness.

If no config file is specified, searches for configuration in the default locations:
  - ./cpe.yaml or ./cpe.yml (current directory)
  - ~/.config/cpe/cpe.yaml or ~/.config/cpe/cpe.yml (user config directory)

Exit codes:
  0 - Configuration is valid
  1 - Configuration has errors`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var configPath string
		if len(args) > 0 {
			configPath = args[0]
		}

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}

		return commands.ConfigLint(cmd.Context(), commands.ConfigLintOptions{
			Config: cfg,
			Writer: os.Stdout,
		})
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configLintCmd)
}
