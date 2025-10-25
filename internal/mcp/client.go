package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os/exec"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/cpe/internal/version"
	"github.com/spachava753/gai"
)

// FilterMcpTools applies tool filtering based on the server configuration
// Returns the filtered tools and a list of filtered-out tool names for logging
func FilterMcpTools(tools []*mcp.Tool, config ServerConfig) ([]*mcp.Tool, []string) {
	// Normalize tool filter value
	toolFilter := config.ToolFilter
	if toolFilter == "" {
		toolFilter = "all" // Default value
	}

	// If "all" mode, return all tools without filtering
	if toolFilter == "all" {
		return tools, nil
	}

	var filteredTools []*mcp.Tool
	var filteredOut []string

	switch toolFilter {
	case "whitelist":
		// Create a set of enabled tools for fast lookup
		enabledSet := make(map[string]bool)
		for _, toolName := range config.EnabledTools {
			enabledSet[toolName] = true
		}

		// Filter tools: only include those in the enabled set
		for _, tool := range tools {
			if enabledSet[tool.Name] {
				filteredTools = append(filteredTools, tool)
			} else {
				filteredOut = append(filteredOut, tool.Name)
			}
		}

	case "blacklist":
		// Create a set of disabled tools for fast lookup
		disabledSet := make(map[string]bool)
		for _, toolName := range config.DisabledTools {
			disabledSet[toolName] = true
		}

		// Filter tools: exclude those in the disabled set
		for _, tool := range tools {
			if !disabledSet[tool.Name] {
				filteredTools = append(filteredTools, tool)
			} else {
				filteredOut = append(filteredOut, tool.Name)
			}
		}
	}

	return filteredTools, filteredOut
}

// ToolCallback implements the gai.ToolCallback interface for MCP tools
type ToolCallback struct {
	ClientSession *mcp.ClientSession
	ToolName      string
	ServerName    string
}

// Call implements the gai.ToolCallback interface
func (c *ToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Parse parameters
	var params map[string]interface{}
	if err := json.Unmarshal(parametersJSON, &params); err != nil {
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           toolCallID,
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(fmt.Sprintf("Error parsing parameters: %v", err)),
				},
			},
		}, nil
	}

	// Call the tool
	result, err := c.ClientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      c.ToolName,
		Arguments: params,
	})
	if err != nil {
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           toolCallID,
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(fmt.Sprintf("Error calling MCP tool %s/%s: %v", c.ServerName, c.ToolName, err)),
				},
			},
		}, nil
	}

	// Convert the MCP CallToolResult to a gai.Message
	blocks := make([]gai.Block, len(result.Content))
	for i, content := range result.Content {
		block := gai.Block{
			ID:        toolCallID,
			BlockType: gai.Content,
		}

		switch c := content.(type) {
		case *mcp.TextContent:
			block.ModalityType = gai.Text
			block.MimeType = "text/plain"
			block.Content = gai.Str(c.Text)
		case *mcp.ImageContent:
			block.ModalityType = gai.Image
			block.MimeType = c.MIMEType
			block.Content = gai.Str(c.Data)
		case *mcp.ResourceLink:
			return gai.Message{}, fmt.Errorf("cannot handle resource links in tool call result")
		default:
			block.ModalityType = gai.Text
			block.MimeType = "text/plain"
			block.Content = gai.Str(fmt.Sprintf("Unknown content type: %T", content))
		}

		blocks[i] = block
	}

	return gai.Message{
		Role:   gai.ToolResult,
		Blocks: blocks,
	}, nil
}

func CreateTransport(config ServerConfig) (transport mcp.Transport, err error) {
	switch config.Type {
	case "stdio", "":
		transport = &mcp.CommandTransport{
			Command: exec.Command(config.Command, config.Args...),
		}
	case "http":
		transport = &mcp.StreamableClientTransport{
			Endpoint:   config.URL,
			HTTPClient: nil,
		}
	case "sse":
		transport = &mcp.SSEClientTransport{
			Endpoint:   config.URL,
			HTTPClient: nil,
		}
	}
	if transport == nil {
		err = fmt.Errorf("transport not supported")
	}
	return
}

// RegisterMCPServerTools registers all tools from all MCP servers with the tool registerer
// It continues registering tools even if some fail, collecting warnings along the way
func RegisterMCPServerTools(ctx context.Context, client *mcp.Client, mcpConfig Config, toolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}) error {
	serverNames := slices.Collect(maps.Keys(mcpConfig.MCPServers))

	// If no servers are configured, return early without an error
	if len(serverNames) == 0 {
		return nil
	}

	registeredTools := make(map[string]string) // Map to track tool names for duplicate detection
	var warnings []string                      // To collect all warnings
	registeredCount := 0                       // Count successful registrations
	totalFilteredOut := 0                      // Count filtered-out tools

	// For each server, get tools and register
	for _, serverName := range serverNames {
		// Get server config for filtering
		serverConfig := mcpConfig.MCPServers[serverName]

		transport, err := CreateTransport(serverConfig)
		if err != nil {
			return err
		}

		clientSession, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return err
		}

		// List tools (client is already initialized in GetClient)
		var tools []*mcp.Tool
		for tool, err := range clientSession.Tools(ctx, nil) {
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to list MCP tools for server %s: %v", serverName, err))
				// Skip this server but continue with others
				continue
			}
			tools = append(tools, tool)
		}

		// Apply tool filtering
		filteredTools, filteredOut := FilterMcpTools(tools, serverConfig)
		totalFilteredOut += len(filteredOut)

		// Log filtering information if tools were filtered
		if len(filteredOut) > 0 {
			toolFilter := serverConfig.ToolFilter
			if toolFilter == "" {
				toolFilter = "all"
			}
			slog.Info("mcp tools filtered", "server", serverName, "filter", toolFilter, "filtered_count", len(filteredOut), "filtered", strings.Join(filteredOut, ", "))
		}

		// Register each filtered tool
		for _, mcpTool := range filteredTools {
			// Check for duplicate tool names
			if existingServer, exists := registeredTools[mcpTool.Name]; exists {
				warnings = append(warnings, fmt.Sprintf(
					"skipping duplicate tool name '%s' in server '%s' (already registered from server '%s')",
					mcpTool.Name, serverName, existingServer))
				continue
			}

			// Create a callback for this tool
			callback := &ToolCallback{
				ClientSession: clientSession,
				ServerName:    serverName,
				ToolName:      mcpTool.Name,
			}

			// Convert the MCP Tool to a gai.Tool
			gaiTool := gai.Tool{
				Name:        mcpTool.Name,
				Description: mcpTool.Description,
				// Convert InputSchema from mcp jsonschema to gai jsonschema
				InputSchema: mcpTool.InputSchema,
			}

			// Register the tool with the callback
			err = toolRegisterer.Register(gaiTool, callback)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf(
					"failed to register MCP tool '%s' from server '%s': %v",
					mcpTool.Name, serverName, err))
				// Skip this tool but continue with others
				continue
			}

			// Track this tool to detect duplicates
			registeredTools[mcpTool.Name] = serverName
			registeredCount++
		}
	}

	// Enhanced logging with filtering information
	if registeredCount > 0 || totalFilteredOut > 0 {
		slog.Info("mcp tools summary", "registered", registeredCount, "filtered_out", totalFilteredOut, "warnings", len(warnings))
	}

	// If we have warnings but also registered at least one tool, print warnings only
	if len(warnings) > 0 {
		if registeredCount > 0 {
			for _, warning := range warnings {
				slog.Warn("mcp tool registration warning", "warning", warning)
			}
			return nil
		}
		return fmt.Errorf("failed to register any MCP tools: %s", strings.Join(warnings, "; "))
	}

	return nil
}

func NewClient() *mcp.Client {
	return mcp.NewClient(
		&mcp.Implementation{
			Name:    "cpe",
			Title:   "CPE",
			Version: version.Get(),
		}, nil,
	)
}
