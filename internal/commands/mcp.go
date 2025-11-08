package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/cpe/internal/config"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
)

// MarkdownRenderer renders markdown to formatted output
type MarkdownRenderer interface {
	Render(markdown string) (string, error)
}

// MCPListServersOptions contains parameters for listing MCP servers
type MCPListServersOptions struct {
	Config config.Config
	Writer io.Writer
}

// MCPListServers lists all configured MCP servers
func MCPListServers(ctx context.Context, opts MCPListServersOptions) error {
	mcpConfig := opts.Config.MCPServers
	if len(mcpConfig) == 0 {
		fmt.Fprintln(opts.Writer, "No MCP servers configured.")
		return nil
	}

	fmt.Fprintln(opts.Writer, "Configured MCP Servers:")
	for name, server := range mcpConfig {
		serverType := server.Type
		if serverType == "" {
			serverType = "stdio"
		}

		timeout := server.Timeout
		if timeout == 0 {
			timeout = 60
		}

		fmt.Fprintf(opts.Writer, "- %s (Type: %s, Timeout: %ds)\n", name, serverType, timeout)

		if serverType == "stdio" && server.Command != "" {
			fmt.Fprintf(opts.Writer, "  Command: %s %s\n", server.Command, strings.Join(server.Args, " "))
		}

		if server.URL != "" {
			fmt.Fprintf(opts.Writer, "  URL: %s\n", server.URL)
		}

		if serverType == "stdio" && len(server.Env) > 0 {
			fmt.Fprintln(opts.Writer, "  Environment Variables:")
			for k, v := range server.Env {
				fmt.Fprintf(opts.Writer, "    %s=%s\n", k, v)
			}
		}
	}

	return nil
}

// MCPInfoOptions contains parameters for getting MCP server info
type MCPInfoOptions struct {
	Config     config.Config
	ServerName string
	Writer     io.Writer
	Timeout    time.Duration
}

// MCPInfo connects to an MCP server and displays its information
func MCPInfo(ctx context.Context, opts MCPInfoOptions) error {
	mcpConfig := opts.Config.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	client := mcpinternal.NewClient()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transport, err := mcpinternal.CreateTransport(serverConfig)
	if err != nil {
		return err
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.Writer, "Connected to server: %s\n", opts.ServerName)

	return cs.Close()
}

// MCPListToolsOptions contains parameters for listing MCP server tools
type MCPListToolsOptions struct {
	Config       config.Config
	ServerName   string
	Writer       io.Writer
	ShowAll      bool
	ShowFiltered bool
	Renderer     MarkdownRenderer
}

// MCPListTools lists tools available on an MCP server
func MCPListTools(ctx context.Context, opts MCPListToolsOptions) error {
	mcpConfig := opts.Config.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	client := mcpinternal.NewClient()

	var allTools []*mcp.Tool
	transport, err := mcpinternal.CreateTransport(serverConfig)
	if err != nil {
		return err
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}

	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return err
		}
		allTools = append(allTools, tool)
	}

	if err := cs.Close(); err != nil {
		return err
	}

	filteredTools, filteredOut := mcpinternal.FilterMcpTools(allTools, serverConfig)

	var toolsToShow []*mcp.Tool
	var title string

	if opts.ShowAll {
		toolsToShow = allTools
		title = fmt.Sprintf("All tools on server '%s' (including filtered)", opts.ServerName)
	} else if opts.ShowFiltered {
		for _, toolName := range filteredOut {
			for _, tool := range allTools {
				if tool.Name == toolName {
					toolsToShow = append(toolsToShow, tool)
					break
				}
			}
		}
		title = fmt.Sprintf("Filtered-out tools on server '%s'", opts.ServerName)
	} else {
		toolsToShow = filteredTools
		title = fmt.Sprintf("Available tools on server '%s'", opts.ServerName)
	}

	var markdownBuilder strings.Builder

	markdownBuilder.WriteString(fmt.Sprintf("# %s\n\n", title))

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
			filteredBadge := ""
			if opts.ShowAll {
				for _, filteredName := range filteredOut {
					if tool.Name == filteredName {
						filteredBadge = " 🚫 *filtered*"
						break
					}
				}
			}

			markdownBuilder.WriteString(fmt.Sprintf("### `%s`%s\n", tool.Name, filteredBadge))
			markdownBuilder.WriteString(tool.Description + "\n\n")

			if tool.InputSchema != nil {
				markdownBuilder.WriteString("**Input Schema:**\n\n")

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

	rendered, err := opts.Renderer.Render(markdownBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprint(opts.Writer, rendered)
	return nil
}

// MCPCallToolOptions contains parameters for calling an MCP tool
type MCPCallToolOptions struct {
	Config     config.Config
	ServerName string
	ToolName   string
	ToolArgs   map[string]interface{}
	Writer     io.Writer
}

// MCPCallTool calls a specific tool on an MCP server
func MCPCallTool(ctx context.Context, opts MCPCallToolOptions) error {
	mcpConfig := opts.Config.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	client := mcpinternal.NewClient()

	transport, err := mcpinternal.CreateTransport(serverConfig)
	if err != nil {
		return err
	}

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	defer cs.Close()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      opts.ToolName,
		Arguments: opts.ToolArgs,
	})
	if err != nil {
		return err
	}

	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			fmt.Fprint(opts.Writer, textContent.Text)
		}
	}

	return nil
}
