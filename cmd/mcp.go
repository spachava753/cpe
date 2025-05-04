package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/mcp"
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
	Long:  `Create an example .cpemcp.json configuration file in the current directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcp.ConfigExists() {
			return fmt.Errorf("MCP configuration file already exists")
		}
		
		if err := mcp.CreateExampleConfig(); err != nil {
			return fmt.Errorf("failed to create example config: %w", err)
		}
		
		fmt.Println("Created example .cpemcp.json configuration file")
		fmt.Println("Edit this file to configure your MCP servers")
		
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
			return err
		}

		if err := config.Validate(); err != nil {
			return err
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
			return err
		}

		if err := config.Validate(); err != nil {
			return err
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
	Use:     "list-tools [server_name]",
	Short:   "List tools available on an MCP server",
	Long:    `Connect to an MCP server and list available tools.`,
	Aliases: []string{"ls-tools"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverName := args[0]
		
		config, err := mcp.LoadConfig()
		if err != nil {
			return err
		}

		if err := config.Validate(); err != nil {
			return err
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
		
		fmt.Printf("Tools available on server '%s':\n", serverName)
		for _, tool := range tools.Tools {
			fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
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
			return err
		}

		if err := config.Validate(); err != nil {
			return err
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
	
	// Add flags to call-tool command
	mcpCallToolCmd.Flags().StringVar(&mcpServerName, "server", "", "MCP server name")
	mcpCallToolCmd.Flags().StringVar(&mcpToolName, "tool", "", "Tool name to call")
	mcpCallToolCmd.Flags().StringVar(&mcpToolArgs, "args", "{}", "Tool arguments in JSON format")
}
