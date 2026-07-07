package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/mcp"
)

var (
	mcpServerName string
	mcpToolName   string
	mcpToolArgs   string
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Inspect configured MCP servers",
	Long:  `Inspect and test Model Context Protocol (MCP) servers configured for a selected CPE model profile.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// mcpListServersCmd represents the 'mcp list-servers' subcommand
var mcpListServersCmd = &cobra.Command{
	Use:     "list-servers",
	Short:   "List configured MCP servers",
	Long:    `List MCP servers defined by the selected model profile in cpe.yaml.`,
	Aliases: []string{"ls-servers"},
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcp.MCPListServersFromConfig(cmd.Context(), mcp.MCPListServersFromConfigOptions{
			ConfigPath: configPath,
			ModelRef:   model,
			Writer:     cmd.OutOrStdout(),
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
		return mcp.MCPInfoFromConfig(cmd.Context(), mcp.MCPInfoFromConfigOptions{
			ConfigPath: configPath,
			ModelRef:   model,
			ServerName: args[0],
			Writer:     cmd.OutOrStdout(),
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

  # Show all tools including filtered ones
  cpe mcp list-tools my-server --show-all
  
  # Show only filtered-out tools
  cpe mcp list-tools my-server --show-filtered`,
	Aliases: []string{"ls-tools"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		showAll, _ := cmd.Flags().GetBool("show-all")
		showFiltered, _ := cmd.Flags().GetBool("show-filtered")

		return mcp.MCPListToolsFromConfig(cmd.Context(), mcp.MCPListToolsFromConfigOptions{
			ConfigPath:   configPath,
			ModelRef:     model,
			ServerName:   args[0],
			Writer:       cmd.OutOrStdout(),
			ShowAll:      showAll,
			ShowFiltered: showFiltered,
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

		toolArgs := make(map[string]any)
		if mcpToolArgs != "" {
			if err := json.Unmarshal([]byte(mcpToolArgs), &toolArgs); err != nil {
				return fmt.Errorf("invalid tool arguments JSON: %w", err)
			}
		}

		return mcp.MCPCallToolFromConfig(cmd.Context(), mcp.MCPCallToolFromConfigOptions{
			ConfigPath: configPath,
			ModelRef:   model,
			ServerName: mcpServerName,
			ToolName:   mcpToolName,
			ToolArgs:   toolArgs,
			Writer:     cmd.OutOrStdout(),
		})
	},
}

// mcpCodeDescCmd represents the 'mcp code-desc' subcommand
var mcpCodeDescCmd = &cobra.Command{
	Use:   "code-desc",
	Short: "Print the execute_go_code tool description",
	Long: `Generate and print the description for the execute_go_code tool.

This shows exactly what description would be provided to the LLM when code mode
is enabled. Code mode executes generated Go code and does not expose MCP server
tools as generated Go function bindings.`,
	Example: `  # Print code mode description with default model's settings
  cpe mcp code-desc

  # Print code mode description for a specific model
  cpe mcp code-desc --model sonnet`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcp.MCPCodeDescFromConfig(cmd.Context(), mcp.MCPCodeDescFromConfigOptions{
			ConfigPath: configPath,
			ModelRef:   model,
			Writer:     cmd.OutOrStdout(),
		})
	},
}

func init() {
	mcpCmd.PersistentFlags().StringVarP(&model, "model", "m", DefaultModel, "Model profile ref whose MCP servers should be inspected (env: CPE_MODEL)")

	rootCmd.AddCommand(mcpCmd)

	// Add subcommands to mcp command
	mcpCmd.AddCommand(mcpListServersCmd)
	mcpCmd.AddCommand(mcpInfoCmd)
	mcpCmd.AddCommand(mcpListToolsCmd)
	mcpCmd.AddCommand(mcpCallToolCmd)
	mcpCmd.AddCommand(mcpCodeDescCmd)

	// Add flags to mcp list-tools command
	mcpListToolsCmd.Flags().Bool("show-all", false, "Show all tools including filtered ones")
	mcpListToolsCmd.Flags().Bool("show-filtered", false, "Show only filtered-out tools")

	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}
