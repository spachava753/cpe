package cmd

import (
	"fmt"

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

		// Load and validate config (automatically validates)
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}

		fmt.Printf("✓ Configuration is valid\n")
		fmt.Printf("  Models: %d\n", len(cfg.Models))
		if len(cfg.MCPServers) > 0 {
			fmt.Printf("  MCP Servers: %d\n", len(cfg.MCPServers))
		}
		if cfg.GetDefaultModel() != "" {
			fmt.Printf("  Default Model: %s\n", cfg.GetDefaultModel())
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configLintCmd)
}
