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
	"time"

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
	Timeout    time.Duration
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

	client := mcpinternal.NewClient()

	transport, err := mcpinternal.CreateTransport(ctx, serverConfig)
	if err != nil {
		return err
	}

	var operationCtx context.Context
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		operationCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	} else {
		operationCtx, cancel = mcpinternal.WithServerTimeout(ctx, serverConfig)
	}
	defer cancel()

	cs, err := client.Connect(operationCtx, transport, nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.Writer, "Connected to server: %s\n", opts.ServerName)

	return cs.Close()
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

	client := mcpinternal.NewClient()

	var allTools []*mcp.Tool
	transport, err := mcpinternal.CreateTransport(ctx, serverConfig)
	if err != nil {
		return err
	}

	operationCtx, cancel := mcpinternal.WithServerTimeout(ctx, serverConfig)
	defer cancel()

	cs, err := client.Connect(operationCtx, transport, nil)
	if err != nil {
		return err
	}
	defer cs.Close()

	for tool, err := range cs.Tools(operationCtx, nil) {
		if err != nil {
			return err
		}
		allTools = append(allTools, tool)
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

	markdownBuilder.WriteString("**Filter mode:** `" + filterMode + "`")

	if len(serverConfig.EnabledTools) > 0 {
		markdownBuilder.WriteString(" | **Enabled tools:** `" + strings.Join(serverConfig.EnabledTools, "`, `") + "`")
	}
	if len(serverConfig.DisabledTools) > 0 {
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
				if slices.Contains(filteredOut, tool.Name) {
					filteredBadge = " 🚫 *filtered*"
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

			if tool.OutputSchema != nil {
				markdownBuilder.WriteString("**Output Schema:**\n\n")

				var schemaJSON bytes.Buffer
				encoder := json.NewEncoder(&schemaJSON)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(tool.OutputSchema); err != nil {
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

	client := mcpinternal.NewClient()

	transport, err := mcpinternal.CreateTransport(ctx, serverConfig)
	if err != nil {
		return err
	}

	operationCtx, cancel := mcpinternal.WithServerTimeout(ctx, serverConfig)
	defer cancel()

	cs, err := client.Connect(operationCtx, transport, nil)
	if err != nil {
		return err
	}
	defer cs.Close()

	result, err := cs.CallTool(operationCtx, &mcp.CallToolParams{
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
	var markdownBuilder strings.Builder
	markdownBuilder.WriteString("# execute_go_code Tool Description\n\n")

	if opts.CodeMode == nil || !opts.CodeMode.Enabled {
		markdownBuilder.WriteString("> **Note:** Code mode is not enabled in current configuration.\n\n")
	}

	if len(excludedToolNames) > 0 {
		markdownBuilder.WriteString("**Excluded tools:** `" + strings.Join(excludedToolNames, "`, `") + "`\n\n")
	}

	markdownBuilder.WriteString("**Code mode tools:** " + strconv.Itoa(len(allCodeModeTools)) + "\n\n")
	markdownBuilder.WriteString("---\n\n")
	markdownBuilder.WriteString(description)

	rendered, err := opts.Renderer.Render(markdownBuilder.String())
	if err != nil {
		return fmt.Errorf("failed to render markdown: %w", err)
	}

	fmt.Fprintln(opts.Writer, rendered)
	return nil
}
