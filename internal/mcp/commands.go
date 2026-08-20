package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/render"
)

const serverTypeStdio = "stdio"

// mcpListServersOptions contains parameters for listing MCP servers
type mcpListServersOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	Writer     io.Writer
}

// mcpListServers lists all configured MCP servers
func mcpListServers(ctx context.Context, opts mcpListServersOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		fmt.Fprintln(opts.Writer, "No MCP servers configured.")
		return nil
	}

	fmt.Fprintln(opts.Writer, "Configured MCP Servers:")
	for name, server := range mcpConfig {
		serverType := effectiveServerType(server)
		timeout := int(effectiveServerTimeout(server).Seconds())

		fmt.Fprintf(opts.Writer, "- %s (Type: %s, Timeout: %ds)\n", name, serverType, timeout)

		if serverType == serverTypeStdio && server.Command != "" {
			fmt.Fprintf(opts.Writer, "  Command: %s %s\n", server.Command, strings.Join(server.Args, " "))
		}

		if server.URL != "" {
			fmt.Fprintf(opts.Writer, "  URL: %s\n", server.URL)
		}

		if serverType == serverTypeStdio && len(server.Env) > 0 {
			fmt.Fprintln(opts.Writer, "  Environment Variables:")
			for k, v := range server.Env {
				fmt.Fprintf(opts.Writer, "    %s=%s\n", k, v)
			}
		}
	}

	return nil
}

// mcpInfoOptions contains parameters for getting MCP server info
type mcpInfoOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	ServerName string
	Writer     io.Writer
}

// mcpInfo connects to an MCP server and displays its information
func mcpInfo(ctx context.Context, opts mcpInfoOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	connectCtx, cancel := withServerTimeout(ctx, serverConfig)
	defer cancel()

	conn, err := connectServer(connectCtx, opts.ServerName, serverConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Fprintf(opts.Writer, "Connected to server: %s\n", opts.ServerName)

	return nil
}

// mcpListToolsOptions contains parameters for listing MCP server tools
type mcpListToolsOptions struct {
	MCPServers   map[string]mcpconfig.ServerConfig
	ServerName   string
	Writer       io.Writer
	ShowAll      bool
	ShowFiltered bool
	Renderer     render.Iface
}

// mcpListTools lists tools available on an MCP server
func mcpListTools(ctx context.Context, opts mcpListToolsOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	conn, err := connectAndListServer(ctx, opts.ServerName, serverConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	allTools := conn.AllTools
	filteredTools := conn.Tools
	filteredOut := conn.FilteredOut

	var toolsToShow []*mcpsdk.Tool
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

	var mdBuilder strings.Builder

	fmt.Fprintf(&mdBuilder, "# %s\n\n", title)

	// Infer filter mode from which list is populated
	var filterMode string
	switch {
	case len(serverConfig.EnabledTools) > 0:
		filterMode = "whitelist"
	case len(serverConfig.DisabledTools) > 0:
		filterMode = "blacklist"
	default:
		filterMode = "all"
	}

	mdBuilder.WriteString("**Filter mode:** `")
	mdBuilder.WriteString(filterMode)
	mdBuilder.WriteString("`")

	if len(serverConfig.EnabledTools) > 0 {
		mdBuilder.WriteString(" | **Enabled tools:** `")
		mdBuilder.WriteString(strings.Join(serverConfig.EnabledTools, "`, `"))
		mdBuilder.WriteString("`")
	}
	if len(serverConfig.DisabledTools) > 0 {
		mdBuilder.WriteString(" | **Disabled tools:** `")
		mdBuilder.WriteString(strings.Join(serverConfig.DisabledTools, "`, `"))
		mdBuilder.WriteString("`")
	}

	mdBuilder.WriteString("\n**Total tools:** ")
	mdBuilder.WriteString(strconv.Itoa(len(allTools)))
	mdBuilder.WriteString(" | **Available:** ")
	mdBuilder.WriteString(strconv.Itoa(len(filteredTools)))
	mdBuilder.WriteString(" | **Filtered out:** ")
	mdBuilder.WriteString(strconv.Itoa(len(filteredOut)))
	mdBuilder.WriteString("\n\n")

	if len(toolsToShow) == 0 {
		mdBuilder.WriteString("*No tools to display.*\n")
	} else {
		for _, tool := range toolsToShow {
			filteredBadge := ""
			if opts.ShowAll {
				if slices.Contains(filteredOut, tool.Name) {
					filteredBadge = " 🚫 *filtered*"
				}
			}

			fmt.Fprintf(&mdBuilder, "### `%s`%s\n", tool.Name, filteredBadge)
			mdBuilder.WriteString(tool.Description)
			mdBuilder.WriteString("\n\n")

			if tool.InputSchema != nil {
				mdBuilder.WriteString("**Input Schema:**\n\n")

				var schemaJSON bytes.Buffer
				encoder := json.NewEncoder(&schemaJSON)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(tool.InputSchema); err != nil {
					mdBuilder.WriteString("```json\n" + "Error encoding schema: ")
					mdBuilder.WriteString(err.Error())
					mdBuilder.WriteString("\n```\n\n")
				} else {
					mdBuilder.WriteString("```json\n")
					mdBuilder.WriteString(schemaJSON.String())
					mdBuilder.WriteString("\n```\n\n")
				}
			}

			if tool.OutputSchema != nil {
				mdBuilder.WriteString("**Output Schema:**\n\n")

				var schemaJSON bytes.Buffer
				encoder := json.NewEncoder(&schemaJSON)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(tool.OutputSchema); err != nil {
					mdBuilder.WriteString("```json\n" + "Error encoding schema: ")
					mdBuilder.WriteString(err.Error())
					mdBuilder.WriteString("\n```\n\n")
				} else {
					mdBuilder.WriteString("```json\n")
					mdBuilder.WriteString(schemaJSON.String())
					mdBuilder.WriteString("\n```\n\n")
				}
			}
		}
	}

	rendered, err := opts.Renderer.Render(mdBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprint(opts.Writer, rendered)
	return nil
}

// mcpCallToolOptions contains parameters for calling an MCP tool
type mcpCallToolOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	ServerName string
	ToolName   string
	ToolArgs   map[string]any
	Writer     io.Writer
}

// mcpCallTool calls a specific tool on an MCP server
func mcpCallTool(ctx context.Context, opts mcpCallToolOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	operationCtx, cancel := withServerTimeout(ctx, serverConfig)
	defer cancel()

	conn, err := connectServer(operationCtx, opts.ServerName, serverConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	result, err := conn.ClientSession.CallTool(operationCtx, &mcpsdk.CallToolParams{
		Name:      opts.ToolName,
		Arguments: opts.ToolArgs,
	})
	if err != nil {
		return err
	}

	for _, content := range result.Content {
		if textContent, ok := content.(*mcpsdk.TextContent); ok {
			fmt.Fprint(opts.Writer, textContent.Text)
		}
	}
	if result.IsError {
		return result.GetError()
	}

	return nil
}
