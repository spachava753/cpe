package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/render"
)

const serverTypeStdio = "stdio"

// MCPListServersOptions contains parameters for listing MCP servers
type MCPListServersOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	Writer     io.Writer
}

// MCPListServers lists all configured MCP servers
func MCPListServers(ctx context.Context, opts MCPListServersOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		fmt.Fprintln(opts.Writer, "No MCP servers configured.")
		return nil
	}

	fmt.Fprintln(opts.Writer, "Configured MCP Servers:")
	for name, server := range mcpConfig {
		serverType := mcpinternal.EffectiveServerType(server)
		timeout := int(mcpinternal.EffectiveServerTimeout(server).Seconds())

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

// MCPInfoOptions contains parameters for getting MCP server info
type MCPInfoOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	ServerName string
	Writer     io.Writer
}

// MCPInfo connects to an MCP server and displays its information
func MCPInfo(ctx context.Context, opts MCPInfoOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	connectCtx, cancel := mcpinternal.WithServerTimeout(ctx, serverConfig)
	defer cancel()

	conn, err := mcpinternal.ConnectServer(connectCtx, opts.ServerName, serverConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Fprintf(opts.Writer, "Connected to server: %s\n", opts.ServerName)

	return nil
}

// MCPListToolsOptions contains parameters for listing MCP server tools
type MCPListToolsOptions struct {
	MCPServers   map[string]mcpconfig.ServerConfig
	ServerName   string
	Writer       io.Writer
	ShowAll      bool
	ShowFiltered bool
	Renderer     render.Iface
}

// MCPListTools lists tools available on an MCP server
func MCPListTools(ctx context.Context, opts MCPListToolsOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	conn, err := mcpinternal.ConnectAndListServer(ctx, opts.ServerName, serverConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	allTools := conn.AllTools
	filteredTools := conn.Tools
	filteredOut := conn.FilteredOut

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

// MCPCallToolOptions contains parameters for calling an MCP tool
type MCPCallToolOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	ServerName string
	ToolName   string
	ToolArgs   map[string]any
	Writer     io.Writer
}

// MCPCallTool calls a specific tool on an MCP server
func MCPCallTool(ctx context.Context, opts MCPCallToolOptions) error {
	mcpConfig := opts.MCPServers
	if len(mcpConfig) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	serverConfig, exists := mcpConfig[opts.ServerName]
	if !exists {
		return fmt.Errorf("server '%s' not found in configuration", opts.ServerName)
	}

	operationCtx, cancel := mcpinternal.WithServerTimeout(ctx, serverConfig)
	defer cancel()

	conn, err := mcpinternal.ConnectServer(operationCtx, opts.ServerName, serverConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	result, err := conn.ClientSession.CallTool(operationCtx, &mcp.CallToolParams{
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
	if result.IsError {
		return result.GetError()
	}

	return nil
}

// MCPCodeDescOptions contains parameters for generating code mode description
type MCPCodeDescOptions struct {
	MCPServers map[string]mcpconfig.ServerConfig
	CodeMode   *config.CodeModeConfig
	Writer     io.Writer
	Renderer   render.Iface
}

// MCPCodeDesc generates and prints the execute_go_code tool description
func MCPCodeDesc(ctx context.Context, opts MCPCodeDescOptions) error {
	// Use InitializeConnections instead of FetchTools
	mcpState, err := mcpinternal.InitializeConnections(ctx, opts.MCPServers)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP connections: %w", err)
	}
	defer mcpState.Close()

	// Get excluded tool names from code mode config
	var excludedToolNames []string
	if opts.CodeMode != nil && opts.CodeMode.ExcludedTools != nil {
		excludedToolNames = opts.CodeMode.ExcludedTools
	}

	// Partition tools - only code mode tools will be included in the description
	codeModeServers, _ := codemode.PartitionTools(mcpState, excludedToolNames)

	// Collect all code mode tools
	var allCodeModeTools []*mcp.Tool
	for _, serverInfo := range codeModeServers {
		allCodeModeTools = append(allCodeModeTools, serverInfo.Tools...)
	}

	// Generate the description
	description, err := codemode.GenerateExecuteGoCodeDescription(allCodeModeTools)
	if err != nil {
		return fmt.Errorf("failed to generate description: %w", err)
	}

	// Format as markdown for rendering
	var mdBuilder strings.Builder
	mdBuilder.WriteString("# execute_go_code Tool Description\n\n")

	if opts.CodeMode == nil || !opts.CodeMode.Enabled {
		mdBuilder.WriteString("> **Note:** Code mode is not enabled in current configuration.\n\n")
	}

	if len(excludedToolNames) > 0 {
		mdBuilder.WriteString("**Excluded tools:** `")
		mdBuilder.WriteString(strings.Join(excludedToolNames, "`, `"))
		mdBuilder.WriteString("`\n\n")
	}

	mdBuilder.WriteString("**Code mode tools:** ")
	mdBuilder.WriteString(strconv.Itoa(len(allCodeModeTools)))
	mdBuilder.WriteString("\n\n")
	mdBuilder.WriteString("---\n\n")
	mdBuilder.WriteString(description)

	rendered, err := opts.Renderer.Render(mdBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprintln(opts.Writer, rendered)
	return nil
}
