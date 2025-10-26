package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/cpe/internal/config"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpServerName string
	mcpToolName   string
	mcpToolArgs   string
)

func getGlamourRenderer() (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(120),
	)
}

// getMCPConfig loads the unified configuration and returns the MCP configuration
func getMCPConfig() (*mcpinternal.Config, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	mcpConfig := &mcpinternal.Config{
		MCPServers: cfg.MCPServers,
	}

	if mcpConfig.MCPServers == nil {
		mcpConfig.MCPServers = make(map[string]mcpinternal.ServerConfig)
	}

	return mcpConfig, nil
}

// mcpInitCmd represents the 'mcp init' subcommand
var mcpInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize MCP configuration",
	Long:  `Create example MCP configuration files in the current directory with different formats.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpinternal.ConfigExists() {
			return fmt.Errorf("MCP configuration file already exists")
		}

		if err := mcpinternal.CreateExampleConfig(); err != nil {
			return fmt.Errorf("failed to create example configs: %w", err)
		}

		fmt.Println(`Created example MCP configuration files:
- .cpemcp.json (JSON format)

You can also use a YAML file. Edit only one file to configure your MCP servers.
CPE will automatically use the first one it finds in the following order:
1. .cpemcp.json
2. .cpemcp.yaml
3. .cpemcp.yml
Note: Configuration files are only searched for in the current directory.`)

		return nil
	},
}

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol client",
	Long:  `Interact with Model Context Protocol (MCP) servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// If no subcommand is specified, show help
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
		config, err := getMCPConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			fmt.Println("No MCP servers configured.")
			fmt.Println("Use 'cpe mcp init' to create an example configuration.")
			return nil
		}

		fmt.Println("Configured MCP Servers:")
		for name, server := range config.MCPServers {
			serverType := server.Type
			if serverType == "" {
				serverType = "stdio"
			}

			// Show timeout (default or configured)
			timeout := server.Timeout
			if timeout == 0 {
				timeout = 60 // Default timeout
			}

			fmt.Printf("- %s (Type: %s, Timeout: %ds)\n", name, serverType, timeout)

			// Only show command for stdio servers
			if serverType == "stdio" && server.Command != "" {
				fmt.Printf("  Command: %s %s\n", server.Command, strings.Join(server.Args, " "))
			}

			if server.URL != "" {
				fmt.Printf("  URL: %s\n", server.URL)
			}

			// Show environment variables for stdio servers
			if (serverType == "stdio") && len(server.Env) > 0 {
				fmt.Println("  Environment Variables:")
				for k, v := range server.Env {
					fmt.Printf("    %s=%s\n", k, v)
				}
			}
		}

		return nil
	},
}

// mcpInfoCmd represents the 'mcp info' subcommand
var mcpInfoCmd = &cobra.Command{
	Use:   "info [server_name]",
	Short: "Get information about an MCP server",
	Long:  `Initialize connection to an MCP server and show its information.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]

		config, err := getMCPConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			return fmt.Errorf("no MCP servers configured. Use 'cpe mcp init' to create an example configuration")
		}

		if _, exists := config.MCPServers[serverName]; !exists {
			return fmt.Errorf("server '%s' not found in configuration", serverName)
		}

		client := mcpinternal.NewClient()

		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		// Get client to trigger initialization
		transport, err := mcpinternal.CreateTransport(config.MCPServers[serverName])
		if err != nil {
			return err
		}
		cs, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Connected to server: %s\n", serverName)

		return cs.Close()
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
		serverName := args[0]

		config, err := getMCPConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			return fmt.Errorf("no MCP servers configured. Use 'cpe mcp init' to create an example configuration")
		}

		serverConfig, exists := config.MCPServers[serverName]
		if !exists {
			return fmt.Errorf("server '%s' not found in configuration", serverName)
		}

		client := mcpinternal.NewClient()

		var allTools []*mcp.Tool
		transport, err := mcpinternal.CreateTransport(serverConfig)
		if err != nil {
			return err
		}
		cs, err := client.Connect(cmd.Context(), transport, nil)
		if err != nil {
			return err
		}
		for tool, err := range cs.Tools(cmd.Context(), nil) {
			if err != nil {
				return err
			}
			allTools = append(allTools, tool)
		}
		if err := cs.Close(); err != nil {
			return err
		}

		// Get flags
		showAll, _ := cmd.Flags().GetBool("show-all")
		showFiltered, _ := cmd.Flags().GetBool("show-filtered")

		// Apply filtering to understand what would be filtered
		filteredTools, filteredOut := mcpinternal.FilterMcpTools(allTools, serverConfig)

		// Determine which tools to show based on flags
		var toolsToShow []*mcp.Tool
		var title string

		if showAll {
			toolsToShow = allTools
			title = fmt.Sprintf("All tools on server '%s' (including filtered)", serverName)
		} else if showFiltered {
			// Create tool objects for filtered-out tools
			for _, toolName := range filteredOut {
				for _, tool := range allTools {
					if tool.Name == toolName {
						toolsToShow = append(toolsToShow, tool)
						break
					}
				}
			}
			title = fmt.Sprintf("Filtered-out tools on server '%s'", serverName)
		} else {
			toolsToShow = filteredTools
			title = fmt.Sprintf("Available tools on server '%s'", serverName)
		}

		// Build markdown content
		var markdownBuilder strings.Builder

		// Header
		markdownBuilder.WriteString(fmt.Sprintf("# %s\n\n", title))

		// Filter information
		toolFilter := serverConfig.ToolFilter
		if toolFilter == "" {
			toolFilter = "all"
		}

		markdownBuilder.WriteString("**Filter mode:** `" + toolFilter + "`")

		if toolFilter == "whitelist" && len(serverConfig.EnabledTools) > 0 {
			markdownBuilder.WriteString(" | **Enabled tools:** `" + strings.Join(serverConfig.EnabledTools, "`, `") + "`")
		}
		if toolFilter == "blacklist" && len(serverConfig.DisabledTools) > 0 {
			markdownBuilder.WriteString(" | **Disabled tools:** `" + strings.Join(serverConfig.DisabledTools, "`, `") + "`")
		}

		markdownBuilder.WriteString("\n**Total tools:** " + strconv.Itoa(len(allTools)) +
			" | **Available:** " + strconv.Itoa(len(filteredTools)) +
			" | **Filtered out:** " + strconv.Itoa(len(filteredOut)) + "\n\n")

		if len(toolsToShow) == 0 {
			markdownBuilder.WriteString("*No tools to display.*\n")
		} else {
			for _, tool := range toolsToShow {
				// Mark filtered tools with a badge
				filteredBadge := ""
				if showAll {
					for _, filteredName := range filteredOut {
						if tool.Name == filteredName {
							filteredBadge = " 🚫 *filtered*"
							break
						}
					}
				}

				markdownBuilder.WriteString(fmt.Sprintf("### `%s`%s\n", tool.Name, filteredBadge))
				markdownBuilder.WriteString(tool.Description + "\n\n")

				// Add input schema
				if tool.InputSchema != nil {
					markdownBuilder.WriteString("**Input Schema:**\n\n")

					// Create a proper JSON code block with indentation
					var schemaJSON bytes.Buffer
					encoder := json.NewEncoder(&schemaJSON)
					encoder.SetIndent("", "  ")
					if err := encoder.Encode(tool.InputSchema); err != nil {
						markdownBuilder.WriteString("```json\n" + "Error encoding schema: " + err.Error() + "\n```\n\n")
					} else {
						markdownBuilder.WriteString("```json\n" + schemaJSON.String() + "\n```\n\n")
					}
				}
			}
		}

		// Render with glamour
		renderer, err := getGlamourRenderer()
		if err != nil {
			return fmt.Errorf("failed to create glamour renderer: %w", err)
		}

		rendered, err := renderer.Render(markdownBuilder.String())
		if err != nil {
			return fmt.Errorf("failed to render markdown: %w", err)
		}

		fmt.Print(rendered)
		return nil
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

		config, err := getMCPConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcpinternal.Config{MCPServers: make(map[string]mcpinternal.ServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			return fmt.Errorf("no MCP servers configured. Use 'cpe mcp init' to create an example configuration")
		}

		if _, exists := config.MCPServers[mcpServerName]; !exists {
			return fmt.Errorf("server '%s' not found in configuration", mcpServerName)
		}

		client := mcpinternal.NewClient()

		// Parse tool args
		toolArgs := make(map[string]interface{})
		if mcpToolArgs != "" {
			if err := json.Unmarshal([]byte(mcpToolArgs), &toolArgs); err != nil {
				return fmt.Errorf("invalid tool arguments JSON: %w", err)
			}
		}

		transport, err := mcpinternal.CreateTransport(config.MCPServers[mcpServerName])
		if err != nil {
			return err
		}

		cs, err := client.Connect(cmd.Context(), transport, nil)
		if err != nil {
			return err
		}
		defer cs.Close()

		// Call the tool (client is already initialized in GetClient)
		result, err := cs.CallTool(cmd.Context(), &mcp.CallToolParams{
			Name:      mcpToolName,
			Arguments: toolArgs,
		})
		if err != nil {
			return err
		}

		// Print the result - the result is a mcp.CallToolResult
		// Extract text content from content
		for _, content := range result.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				fmt.Print(textContent.Text)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)

	// Add subcommands to mcp command
	mcpCmd.AddCommand(mcpInitCmd)
	mcpCmd.AddCommand(mcpListServersCmd)
	mcpCmd.AddCommand(mcpInfoCmd)
	mcpCmd.AddCommand(mcpListToolsCmd)
	mcpCmd.AddCommand(mcpCallToolCmd)

	// Add flags to mcp list-tools command
	mcpListToolsCmd.Flags().Bool("show-all", false, "Show all tools including filtered ones")
	mcpListToolsCmd.Flags().Bool("show-filtered", false, "Show only filtered-out tools")

	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}
