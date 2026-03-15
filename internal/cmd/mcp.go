package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
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
		return commands.MCPListServersFromConfig(cmd.Context(), commands.MCPListServersFromConfigOptions{
			ConfigPath: configPath,
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
		return commands.MCPInfoFromConfig(cmd.Context(), commands.MCPInfoFromConfigOptions{
			ConfigPath: configPath,
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
  
  # List tools with JSON schema format
  cpe mcp list-tools my-server --json
  
  # Show all tools including filtered ones
  cpe mcp list-tools my-server --show-all
  
  # Show only filtered-out tools
  cpe mcp list-tools my-server --show-filtered`,
	Aliases: []string{"ls-tools"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		showAll, _ := cmd.Flags().GetBool("show-all")
		showFiltered, _ := cmd.Flags().GetBool("show-filtered")

		return commands.MCPListToolsFromConfig(cmd.Context(), commands.MCPListToolsFromConfigOptions{
			ConfigPath:   configPath,
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

		return commands.MCPCallToolFromConfig(cmd.Context(), commands.MCPCallToolFromConfigOptions{
			ConfigPath: configPath,
			ServerName: mcpServerName,
			ToolName:   mcpToolName,
			ToolArgs:   toolArgs,
			Writer:     cmd.OutOrStdout(),
		})
	},
}

// mcpServeCmd starts MCP server mode (single subagent tool over stdio).
// In this mode stdout is reserved for MCP protocol traffic, so diagnostics must
// flow through stderr from lower layers.
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
		// Server mode requires an explicit config to guarantee deterministic tool
		// name/description/schema exposure; default config search is disabled.
		configFlag := cmd.Root().PersistentFlags().Lookup("config")
		if configFlag == nil || !configFlag.Changed {
			return fmt.Errorf("--config flag is required for mcp serve")
		}

		return commands.MCPServe(cmd.Context(), commands.MCPServeOptions{
			ConfigPath: configPath,
		})
	},
}

// mcpCodeDescCmd represents the 'mcp code-desc' subcommand
var mcpCodeDescCmd = &cobra.Command{
	Use:   "code-desc",
	Short: "Print the execute_go_code tool description",
	Long: `Generate and print the description for the execute_go_code tool.

This shows exactly what description would be provided to the LLM when code mode 
is enabled, including all MCP tools converted to Go functions. The output 
respects the codeMode.excludedTools configuration - excluded tools will not 
appear in the generated Go function definitions.`,
	Example: `  # Print code mode description with default model's settings
  cpe mcp code-desc

  # Print code mode description for a specific model
  cpe mcp code-desc --model sonnet`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.MCPCodeDescFromConfig(cmd.Context(), commands.MCPCodeDescFromConfigOptions{
			ConfigPath: configPath,
			ModelRef:   model,
			Writer:     cmd.OutOrStdout(),
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
	mcpCmd.AddCommand(mcpCodeDescCmd)

	// Add flags to mcp list-tools command
	mcpListToolsCmd.Flags().Bool("show-all", false, "Show all tools including filtered ones")
	mcpListToolsCmd.Flags().Bool("show-filtered", false, "Show only filtered-out tools")

	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}
