package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/commands"
	"github.com/spachava753/cpe/internal/config"
)

var (
	mcpServerName string
	mcpToolName   string
	mcpToolArgs   string
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol client",
	Long:  `Interact with Model Context Protocol (MCP) servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// mcpListServersCmd represents the 'mcp list-servers' subcommand
var mcpListServersCmd = &cobra.Command{
	Use:     "list-servers",
	Short:   "List configured MCP servers",
	Long:    `List all MCP servers defined in .cpemcp.json configuration file.`,
	Aliases: []string{"ls-servers"},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		return commands.MCPListServers(cmd.Context(), commands.MCPListServersOptions{
			MCPServers: cfg.MCPServers,
			Writer:     os.Stdout,
		})
	},
}

// mcpInfoCmd represents the 'mcp info' subcommand
var mcpInfoCmd = &cobra.Command{
	Use:   "info [server_name]",
	Short: "Get information about an MCP server",
	Long:  `Initialize connection to an MCP server and show its information.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		return commands.MCPInfo(cmd.Context(), commands.MCPInfoOptions{
			MCPServers: cfg.MCPServers,
			ServerName: args[0],
			Writer:     os.Stdout,
			Timeout:    30 * time.Second,
		})
	},
}

// mcpListToolsCmd represents the 'mcp list-tools' subcommand
var mcpListToolsCmd = &cobra.Command{
	Use:   "list-tools [server_name]",
	Short: "List tools available on an MCP server",
	Long:  `Connect to an MCP server and list available tools with their input schemas.`,
	Example: `  # List tools with human-readable schema
  cpe mcp list-tools my-server
  
  # List tools with JSON schema format
  cpe mcp list-tools my-server --json
  
  # Show all tools including filtered ones
  cpe mcp list-tools my-server --show-all
  
  # Show only filtered-out tools
  cpe mcp list-tools my-server --show-filtered`,
	Aliases: []string{"ls-tools"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		showAll, _ := cmd.Flags().GetBool("show-all")
		showFiltered, _ := cmd.Flags().GetBool("show-filtered")

		return commands.MCPListTools(cmd.Context(), commands.MCPListToolsOptions{
			MCPServers:   cfg.MCPServers,
			ServerName:   args[0],
			Writer:       os.Stdout,
			ShowAll:      showAll,
			ShowFiltered: showFiltered,
			Renderer:     agent.NewRenderer(),
		})
	},
}

// mcpCallToolCmd represents the 'mcp call-tool' subcommand
var mcpCallToolCmd = &cobra.Command{
	Use:   "call-tool",
	Short: "Call a tool on an MCP server",
	Long:  `Call a specific tool on an MCP server with arguments.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpServerName == "" {
			return fmt.Errorf("--server is required")
		}

		if mcpToolName == "" {
			return fmt.Errorf("--tool is required")
		}

		cfg, err := config.ResolveConfig(configPath, config.RuntimeOptions{})
		if err != nil {
			return err
		}

		toolArgs := make(map[string]any)
		if mcpToolArgs != "" {
			if err := json.Unmarshal([]byte(mcpToolArgs), &toolArgs); err != nil {
				return fmt.Errorf("invalid tool arguments JSON: %w", err)
			}
		}

		return commands.MCPCallTool(cmd.Context(), commands.MCPCallToolOptions{
			MCPServers: cfg.MCPServers,
			ServerName: mcpServerName,
			ToolName:   mcpToolName,
			ToolArgs:   toolArgs,
			Writer:     os.Stdout,
		})
	},
}

// mcpServeCmd represents the 'mcp serve' subcommand
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run CPE as an MCP server",
	Long: `Start CPE as an MCP server that exposes a configured subagent as a tool.

The server communicates via stdio and exposes exactly one tool based on 
the subagent configuration in the provided config file.

This command requires an explicit --config flag pointing to a subagent 
configuration file. The default config search behavior is disabled.`,
	Example: `  # Start the MCP server with a subagent config
  cpe mcp serve --config ./coder_agent.cpe.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// For mcp serve, we require an explicit config path - don't use default search
		configFlag := cmd.Root().PersistentFlags().Lookup("config")
		if configFlag == nil || !configFlag.Changed {
			return fmt.Errorf("--config flag is required for mcp serve")
		}

		return commands.MCPServe(cmd.Context(), commands.MCPServeOptions{
			ConfigPath: configPath,
		})
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)

	// Add subcommands to mcp command
	mcpCmd.AddCommand(mcpListServersCmd)
	mcpCmd.AddCommand(mcpInfoCmd)
	mcpCmd.AddCommand(mcpListToolsCmd)
	mcpCmd.AddCommand(mcpCallToolCmd)
	mcpCmd.AddCommand(mcpServeCmd)

	// Add flags to mcp list-tools command
	mcpListToolsCmd.Flags().Bool("show-all", false, "Show all tools including filtered ones")
	mcpListToolsCmd.Flags().Bool("show-filtered", false, "Show only filtered-out tools")

	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}
