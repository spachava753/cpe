package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/gai"
	"github.com/spf13/cobra"
)

var (
	mcpServerName string
	mcpToolName   string
	mcpToolArgs   string
)

// mcpInitCmd represents the 'mcp init' subcommand
var mcpInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize MCP configuration",
	Long:  `Create example MCP configuration files in the current directory with different formats.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcp.ConfigExists() {
			return fmt.Errorf("MCP configuration file already exists")
		}

		if err := mcp.CreateExampleConfig(); err != nil {
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
		config, err := mcp.LoadConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
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

		config, err := mcp.LoadConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			return fmt.Errorf("no MCP servers configured. Use 'cpe mcp init' to create an example configuration")
		}

		if _, exists := config.MCPServers[serverName]; !exists {
			return fmt.Errorf("server '%s' not found in configuration", serverName)
		}

		clientManager := mcp.NewClientManager(config)
		defer clientManager.Close()

		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		// Get client to trigger initialization
		client, err := clientManager.GetClient(ctx, serverName)
		if err != nil {
			return err
		}

		// Get server info from the client
		serverInfo := client.GetServerInfo()
		fmt.Printf("Server: %s %s\n", serverInfo.Name, serverInfo.Version)

		// Get server capabilities
		serverCaps := client.GetServerCapabilities()

		// Check capabilities
		if serverCaps.Experimental != nil && len(serverCaps.Experimental) > 0 {
			fmt.Println("\nExperimental Capabilities:")
			for name, _ := range serverCaps.Experimental {
				fmt.Printf("- %s\n", name)
			}
		}

		return nil
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

		config, err := mcp.LoadConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			return fmt.Errorf("no MCP servers configured. Use 'cpe mcp init' to create an example configuration")
		}

		serverConfig, exists := config.MCPServers[serverName]
		if !exists {
			return fmt.Errorf("server '%s' not found in configuration", serverName)
		}

		clientManager := mcp.NewClientManager(config)
		defer clientManager.Close()

		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		// List tools (client is already initialized in GetClient)
		allTools, err := clientManager.ListTools(ctx, serverName)
		if err != nil {
			return err
		}

		// Get flags
		showJsonFormat, _ := cmd.Flags().GetBool("json")
		showAll, _ := cmd.Flags().GetBool("show-all")
		showFiltered, _ := cmd.Flags().GetBool("show-filtered")

		// Apply filtering to understand what would be filtered
		filteredTools, filteredOut := mcp.FilterToolsPublic(allTools, serverConfig, serverName)

		// Determine which tools to show based on flags
		var toolsToShow []gai.Tool
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

		// Show filtering information
		toolFilter := serverConfig.ToolFilter
		if toolFilter == "" {
			toolFilter = "all"
		}

		fmt.Printf("%s:\n", title)
		fmt.Printf("Filter mode: %s\n", toolFilter)
		if toolFilter == "whitelist" && len(serverConfig.EnabledTools) > 0 {
			fmt.Printf("Enabled tools: %s\n", strings.Join(serverConfig.EnabledTools, ", "))
		}
		if toolFilter == "blacklist" && len(serverConfig.DisabledTools) > 0 {
			fmt.Printf("Disabled tools: %s\n", strings.Join(serverConfig.DisabledTools, ", "))
		}
		fmt.Printf("Total tools: %d, Available: %d, Filtered out: %d\n\n", len(allTools), len(filteredTools), len(filteredOut))

		if len(toolsToShow) == 0 {
			fmt.Println("No tools to display.")
			return nil
		}

		for _, tool := range toolsToShow {
			// Mark filtered tools with a prefix
			prefix := ""
			if showAll {
				for _, filteredName := range filteredOut {
					if tool.Name == filteredName {
						prefix = "[FILTERED] "
						break
					}
				}
			}

			fmt.Printf("- %s%s: %s\n", prefix, tool.Name, tool.Description)

			// Print input schema
			if tool.InputSchema.Type != gai.Null {
				fmt.Println("  Input Schema:")

				if showJsonFormat {
					// Display raw JSON schema
					schemaBytes, err := json.MarshalIndent(tool.InputSchema, "    ", "  ")
					if err != nil {
						fmt.Printf("    Error marshaling schema: %v\n", err)
					} else {
						fmt.Println(string(schemaBytes))
					}
				} else {
					// Display human-readable format
					fmt.Printf("    Type: %s\n", getPropertyTypeString(tool.InputSchema.Type))

					if len(tool.InputSchema.Required) > 0 {
						fmt.Printf("    Required: %s\n", strings.Join(tool.InputSchema.Required, ", "))
					}

					if len(tool.InputSchema.Properties) > 0 {
						fmt.Println("    Properties:")
						for propName, propDetails := range tool.InputSchema.Properties {
							// Print property details with nested schema info
							printGAIProperty(propName, propDetails, tool.InputSchema.Required, 6)
						}
					}
				}
			}

			fmt.Println() // Add a blank line between tools for better readability
		}

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

		config, err := mcp.LoadConfig()
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if err := config.Validate(); err != nil {
			fmt.Printf("Warning: %v\n", err)
			config = &mcp.ConfigFile{MCPServers: make(map[string]mcp.MCPServerConfig)}
		}

		if len(config.MCPServers) == 0 {
			return fmt.Errorf("no MCP servers configured. Use 'cpe mcp init' to create an example configuration")
		}

		if _, exists := config.MCPServers[mcpServerName]; !exists {
			return fmt.Errorf("server '%s' not found in configuration", mcpServerName)
		}

		clientManager := mcp.NewClientManager(config)
		defer clientManager.Close()

		// Parse tool args
		toolArgs := make(map[string]interface{})
		if mcpToolArgs != "" {
			if err := json.Unmarshal([]byte(mcpToolArgs), &toolArgs); err != nil {
				return fmt.Errorf("invalid tool arguments JSON: %w", err)
			}
		}

		// Call the tool (client is already initialized in GetClient)
		result, err := clientManager.CallTool(cmd.Context(), mcpServerName, mcpToolName, toolArgs)
		if err != nil {
			return err
		}

		// Print the result - the result is a gai.Message
		// Extract text content from blocks
		for _, block := range result.Blocks {
			if block.ModalityType == gai.Text {
				fmt.Print(block.Content)
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
	mcpListToolsCmd.Flags().Bool("json", false, "Show schema in raw JSON format")
	mcpListToolsCmd.Flags().Bool("show-all", false, "Show all tools including filtered ones")
	mcpListToolsCmd.Flags().Bool("show-filtered", false, "Show only filtered-out tools")

	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}

// printConstraint formats and prints schema constraints like anyOf, allOf, etc
func printConstraint(indentStr, key string, value interface{}) {
	// Special handling for anyOf, oneOf, allOf for cleaner output
	if key == "anyOf" || key == "oneOf" || key == "allOf" {
		if types, ok := value.([]interface{}); ok {
			typeStrs := []string{}
			for _, t := range types {
				// Format each type option
				if tMap, ok := t.(map[string]interface{}); ok {
					if tType, ok := tMap["type"]; ok {
						typeStrs = append(typeStrs, fmt.Sprintf("%s", tType))
					} else {
						// For complex type options, use a simplified representation
						typeStrs = append(typeStrs, fmt.Sprintf("%v", tMap))
					}
				} else {
					typeStrs = append(typeStrs, fmt.Sprintf("%v", t))
				}
			}
			fmt.Printf("%s  %s: [%s]\n", indentStr, key, strings.Join(typeStrs, ", "))
		} else {
			fmt.Printf("%s  %s: %v\n", indentStr, key, value)
		}
	} else {
		fmt.Printf("%s  %s: %v\n", indentStr, key, value)
	}
}

// printGAIProperty prints a gai.Property with proper indentation
func printGAIProperty(name string, prop gai.Property, required []string, indent int) {
	indentStr := strings.Repeat(" ", indent)

	// Mark required parameters
	requiredMarker := ""
	for _, req := range required {
		if req == name {
			requiredMarker = " (required)"
			break
		}
	}

	// Get type string
	typeStr := getPropertyTypeString(prop.Type)

	// Handle anyOf case
	if len(prop.AnyOf) > 0 {
		types := []string{}
		for _, anyProp := range prop.AnyOf {
			types = append(types, getPropertyTypeString(anyProp.Type))
		}
		typeStr = fmt.Sprintf("anyOf[%s]", strings.Join(types, ", "))
	}

	// Print the property name, type, and description
	fmt.Printf("%s%s: %s%s", indentStr, name, typeStr, requiredMarker)
	if prop.Description != "" {
		fmt.Printf(" - %s", prop.Description)
	}
	fmt.Println()

	// Print enum values if present
	if len(prop.Enum) > 0 {
		fmt.Printf("%s  enum: [%s]\n", indentStr, strings.Join(prop.Enum, " "))
	}

	// Process additional schema information based on type
	switch prop.Type {
	case gai.Array:
		// Process array items schema
		if prop.Items != nil {
			fmt.Printf("%s  items:\n", indentStr)
			fmt.Printf("%s    type: %s\n", indentStr, getPropertyTypeString(prop.Items.Type))

			// Process item enum values if present
			if len(prop.Items.Enum) > 0 {
				fmt.Printf("%s    enum: [%s]\n", indentStr, strings.Join(prop.Items.Enum, " "))
			}

			// Handle nested properties for object items
			if prop.Items.Type == gai.Object && len(prop.Items.Properties) > 0 {
				fmt.Printf("%s    properties:\n", indentStr)
				for propName, propDetails := range prop.Items.Properties {
					printGAIProperty(propName, propDetails, prop.Items.Required, indent+6)
				}
			}
		}

	case gai.Object:
		// Process object properties
		if len(prop.Properties) > 0 {
			fmt.Printf("%s  properties:\n", indentStr)
			for propName, propDetails := range prop.Properties {
				printGAIProperty(propName, propDetails, prop.Required, indent+4)
			}
		}
	}
}

// getPropertyTypeString converts a gai.PropertyType to a string
func getPropertyTypeString(propType gai.PropertyType) string {
	switch propType {
	case gai.String:
		return "string"
	case gai.Number:
		return "number"
	case gai.Integer:
		return "integer"
	case gai.Boolean:
		return "boolean"
	case gai.Object:
		return "object"
	case gai.Array:
		return "array"
	case gai.Null:
		return "null"
	default:
		return "unknown"
	}
}
