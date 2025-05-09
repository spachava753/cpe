package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpServerName string
	mcpToolName   string
	mcpToolArgs   string
)

// mcpServeCmd represents the 'mcp serve' subcommand
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve CPE native tools as an MCP server",
	Long:  `Expose CPE's native tools through the Model Context Protocol over stdio.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create the ignorer for file operations
		ignorer, err := ignore.LoadIgnoreFiles(".")
		if err != nil {
			return fmt.Errorf("failed to load ignore files: %w", err)
		}

		// Create the MCP server
		mcpServer := agent.NewStdioMCPServer(ignorer)

		// Serve over stdio
		return agent.ServeStdio(mcpServer)
	},
}

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

		fmt.Println("Created example MCP configuration files:")
		fmt.Println("- .cpemcp.json (JSON format)")
		fmt.Println("- .cpemcp.yaml (YAML format)")
		fmt.Println("- .cpemcp.yml (YAML format - alternative extension)")
		fmt.Println("\nYou can use any of these formats. Edit only one file to configure your MCP servers.")
		fmt.Println("CPE will automatically use the first one it finds in the following order:")
		fmt.Println("1. .cpemcp.json")
		fmt.Println("2. .cpemcp.yaml")
		fmt.Println("3. .cpemcp.yml")
		fmt.Println("\nNote: Configuration files are only searched for in the current directory.")

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

		initResult, err := clientManager.InitializeClient(ctx, serverName)
		if err != nil {
			return err
		}

		fmt.Printf("Server: %s %s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
		fmt.Printf("Protocol Version: %s\n", initResult.ProtocolVersion)

		// Check capabilities
		if initResult.Capabilities.Experimental != nil && len(initResult.Capabilities.Experimental) > 0 {
			fmt.Println("\nExperimental Capabilities:")
			for name, _ := range initResult.Capabilities.Experimental {
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
  cpe mcp list-tools my-server --json`,
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

		if _, exists := config.MCPServers[serverName]; !exists {
			return fmt.Errorf("server '%s' not found in configuration", serverName)
		}

		clientManager := mcp.NewClientManager(config)
		defer clientManager.Close()

		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		// Initialize the client
		_, err = clientManager.InitializeClient(ctx, serverName)
		if err != nil {
			return err
		}

		// List tools
		tools, err := clientManager.ListTools(ctx, serverName)
		if err != nil {
			return err
		}

		// Get the JSON flag
		showJsonFormat, _ := cmd.Flags().GetBool("json")

		fmt.Printf("Tools available on server '%s':\n", serverName)
		for _, tool := range tools.Tools {
			fmt.Printf("- %s: %s\n", tool.Name, tool.Description)

			// Print input schema
			if tool.InputSchema.Type != "" {
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
					fmt.Printf("    Type: %s\n", tool.InputSchema.Type)

					if len(tool.InputSchema.Required) > 0 {
						fmt.Printf("    Required: %s\n", strings.Join(tool.InputSchema.Required, ", "))
					}

					if len(tool.InputSchema.Properties) > 0 {
						fmt.Println("    Properties:")
						for propName, propDetails := range tool.InputSchema.Properties {
							// Print property details with nested schema info
							printProperty(propName, propDetails, tool.InputSchema.Required, 6)
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

		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		// Initialize the client
		_, err = clientManager.InitializeClient(ctx, mcpServerName)
		if err != nil {
			return err
		}

		// Parse tool args
		toolArgs := make(map[string]interface{})
		if mcpToolArgs != "" {
			if err := json.Unmarshal([]byte(mcpToolArgs), &toolArgs); err != nil {
				return fmt.Errorf("invalid tool arguments JSON: %w", err)
			}
		}

		// Call the tool
		result, err := clientManager.CallTool(ctx, mcpServerName, mcpToolName, toolArgs)
		if err != nil {
			return err
		}

		// Print the result
		fmt.Print(mcp.PrintContent(result.Content))

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
	mcpCmd.AddCommand(mcpServeCmd) // Add the new serve command

	// Add flags to mcp list-tools command
	mcpListToolsCmd.Flags().Bool("json", false, "Show schema in raw JSON format")

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

// printProperty prints a property and its nested schema recursively with proper indentation
// It handles complex nested structures of any depth in JSON Schema
func printProperty(name string, details interface{}, required []string, indent int) {
	indentStr := strings.Repeat(" ", indent)

	// Handle primitive values (shouldn't happen for well-formed schemas but just in case)
	propMap, ok := details.(map[string]interface{})
	if !ok {
		fmt.Printf("%s%s: %v\n", indentStr, name, details)
		return
	}

	// Extract property type and description
	propType := "object" // Default type if not specified
	propDesc := ""
	enumValues := []string{}

	if t, ok := propMap["type"].(string); ok {
		propType = t
	}

	if d, ok := propMap["description"].(string); ok {
		propDesc = d
	}

	// Extract enum values if present
	if enum, ok := propMap["enum"].([]interface{}); ok && len(enum) > 0 {
		for _, v := range enum {
			if s, ok := v.(string); ok {
				enumValues = append(enumValues, s)
			} else {
				enumValues = append(enumValues, fmt.Sprintf("%v", v))
			}
		}
	}

	// Mark required parameters
	requiredMarker := ""
	for _, req := range required {
		if req == name {
			requiredMarker = " (required)"
			break
		}
	}

	// Print the property name, type, and description
	fmt.Printf("%s%s: %s%s", indentStr, name, propType, requiredMarker)
	if propDesc != "" {
		fmt.Printf(" - %s", propDesc)
	}
	fmt.Println()

	// Print enum values if present
	if len(enumValues) > 0 {
		fmt.Printf("%s  enum: [%s]\n", indentStr, strings.Join(enumValues, " "))
	}

	// Process additional schema information based on type
	switch propType {
	case "array":
		// Process array items schema
		if items, ok := propMap["items"].(map[string]interface{}); ok {
			fmt.Printf("%s  items:\n", indentStr)

			// Get item type
			itemType := "object" // Default if not specified
			if t, ok := items["type"].(string); ok {
				itemType = t
			}

			fmt.Printf("%s    type: %s\n", indentStr, itemType)

			// Process item enum values if present
			if enum, ok := items["enum"].([]interface{}); ok && len(enum) > 0 {
				itemEnumValues := []string{}
				for _, v := range enum {
					if s, ok := v.(string); ok {
						itemEnumValues = append(itemEnumValues, s)
					} else {
						itemEnumValues = append(itemEnumValues, fmt.Sprintf("%v", v))
					}
				}
				if len(itemEnumValues) > 0 {
					fmt.Printf("%s    enum: [%s]\n", indentStr, strings.Join(itemEnumValues, " "))
				}
			}

			// Handle nested properties based on item type
			if itemType == "object" {
				if props, ok := items["properties"].(map[string]interface{}); ok && len(props) > 0 {
					fmt.Printf("%s    properties:\n", indentStr)

					// Extract required fields list
					var itemRequired []string
					if req, ok := items["required"].([]interface{}); ok {
						for _, v := range req {
							if s, ok := v.(string); ok {
								itemRequired = append(itemRequired, s)
							}
						}
					}

					// Process each property
					for propName, propDetails := range props {
						printProperty(propName, propDetails, itemRequired, indent+6)
					}
				} else {
					// This is an "object" type without defined properties
					// Check if it has a reference or additional constraints
					for k, v := range items {
						if k != "type" && k != "description" && k != "enum" {
							printConstraint(indentStr+"    ", k, v)
						}
					}
				}
			} else {
				// For non-object types, print any constraints or additional properties
				for k, v := range items {
					if k != "type" && k != "description" && k != "enum" {
						printConstraint(indentStr+"    ", k, v)
					}
				}
			}
		}

	case "object":
		// Process object properties
		if props, ok := propMap["properties"].(map[string]interface{}); ok && len(props) > 0 {
			fmt.Printf("%s  properties:\n", indentStr)

			// Extract required fields list
			var objRequired []string
			if req, ok := propMap["required"].([]interface{}); ok {
				for _, v := range req {
					if s, ok := v.(string); ok {
						objRequired = append(objRequired, s)
					}
				}
			}

			// Process each property
			for propName, propDetails := range props {
				printProperty(propName, propDetails, objRequired, indent+4)
			}
		} else {
			// This is an "object" type without defined properties
			// It might be a reference, a primitive in disguise, or might have additional constraints
			for k, v := range propMap {
				if k != "type" && k != "description" && k != "enum" {
					printConstraint(indentStr, k, v)
				}
			}
		}

	default:
		// For primitive types, print constraints and additional properties
		for k, v := range propMap {
			if k != "type" && k != "description" && k != "enum" {
				printConstraint(indentStr, k, v)
			}
		}
	}

	// Handle additionalProperties for objects
	if addProps, ok := propMap["additionalProperties"].(map[string]interface{}); ok {
		fmt.Printf("%s  additionalProperties:\n", indentStr)
		addPropsType := "object"
		if t, ok := addProps["type"].(string); ok {
			addPropsType = t
		}
		fmt.Printf("%s    type: %s\n", indentStr, addPropsType)

		// Recursively process additionalProperties if it's an object with properties
		if addPropsType == "object" {
			if nestedProps, ok := addProps["properties"].(map[string]interface{}); ok {
				fmt.Printf("%s    properties:\n", indentStr)

				var addPropsRequired []string
				if req, ok := addProps["required"].([]interface{}); ok {
					for _, v := range req {
						if s, ok := v.(string); ok {
							addPropsRequired = append(addPropsRequired, s)
						}
					}
				}

				for propName, propDetails := range nestedProps {
					printProperty(propName, propDetails, addPropsRequired, indent+6)
				}
			}
		}
	}
}
