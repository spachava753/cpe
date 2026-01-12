package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os/exec"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/version"
)

// headerRoundTripper adds custom headers to HTTP requests
type headerRoundTripper struct {
	headers map[string]string
	next    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for name, value := range h.headers {
		req.Header.Set(name, value)
	}
	if h.next == nil {
		h.next = http.DefaultTransport
	}
	return h.next.RoundTrip(req)
}

// ServerConfig represents the configuration for a single MCP server
type ServerConfig struct {
	Command       string            `json:"command" yaml:"command" validate:"required_if=Type stdio"`
	Args          []string          `json:"args" yaml:"args"`
	Type          string            `json:"type,omitempty" yaml:"type,omitempty" validate:"required,oneof=stdio sse http"`
	URL           string            `json:"url,omitempty" yaml:"url,omitempty" validate:"excluded_if=Type stdio,required_if=Type sse,required_if=Type http,omitempty,https_url|http_url"`
	Timeout       int               `json:"timeout,omitempty" yaml:"timeout,omitempty" validate:"gte=0"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty" validate:"excluded_unless=Type stdio"`
	Headers       map[string]string `json:"headers,omitempty" yaml:"headers,omitempty" validate:"excluded_if=Type stdio"`
	EnabledTools  []string          `json:"enabledTools,omitempty" yaml:"enabledTools,omitempty" validate:"required_if=ToolFilter whitelist,excluded_with=DisabledTools"`
	DisabledTools []string          `json:"disabledTools,omitempty" yaml:"disabledTools,omitempty" validate:"required_if=ToolFilter blacklist,excluded_with=EnabledTools"`
	ToolFilter    string            `json:"toolFilter,omitempty" yaml:"toolFilter,omitempty" validate:"omitempty,oneof=all whitelist blacklist"`
}

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
	var params map[string]any
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
		var block gai.Block

		switch c := content.(type) {
		case *mcp.TextContent:
			block = gai.TextBlock(c.Text)
			block.ID = toolCallID
		case *mcp.ImageContent:
			// ImageContent.Data contains raw bytes (already base64-decoded by json.Unmarshal)
			// ImageBlock will base64-encode them for us
			block = gai.ImageBlock(c.Data, c.MIMEType)
			block.ID = toolCallID
		case *mcp.ResourceLink:
			return gai.Message{}, fmt.Errorf("cannot handle resource links in tool call result")
		default:
			block = gai.TextBlock(fmt.Sprintf("Unknown content type: %T", content))
			block.ID = toolCallID
		}

		blocks[i] = block
	}

	return gai.Message{
		Role:   gai.ToolResult,
		Blocks: blocks,
	}, nil
}

func CreateTransport(config ServerConfig) (transport mcp.Transport, err error) {
	// Create custom HTTP client with headers if specified
	var httpClient *http.Client
	if len(config.Headers) > 0 && (config.Type == "http" || config.Type == "sse") {
		httpClient = &http.Client{
			Transport: &headerRoundTripper{
				headers: config.Headers,
			},
		}
	}

	switch config.Type {
	case "stdio", "":
		transport = &mcp.CommandTransport{
			Command: exec.Command(config.Command, config.Args...),
		}
	case "http":
		transport = &mcp.StreamableClientTransport{
			Endpoint:   config.URL,
			HTTPClient: httpClient,
		}
	case "sse":
		transport = &mcp.SSEClientTransport{
			Endpoint:   config.URL,
			HTTPClient: httpClient,
		}
	}
	if transport == nil {
		err = fmt.Errorf("transport not supported")
	}
	return
}

// ToolData holds tool information with its callback and original MCP tool
type ToolData struct {
	Tool         gai.Tool
	MCPTool      *mcp.Tool
	ToolCallback gai.ToolCallback
}

// FetchTools retrieves all tools from all MCP servers and returns them with their callbacks.
// Returns a map keyed by server name, where each value is a slice of tools from that server.
// It continues fetching tools even if some fail, collecting warnings along the way.
func FetchTools(ctx context.Context, client *mcp.Client, mcpServers map[string]ServerConfig) (map[string][]ToolData, error) {
	serverNames := slices.Collect(maps.Keys(mcpServers))

	// If no servers are configured, return early without an error
	if len(serverNames) == 0 {
		return make(map[string][]ToolData), nil
	}

	registeredTools := make(map[string]string) // Map to track tool names for duplicate detection
	var warnings []string                      // To collect all warnings
	registeredCount := 0                       // Count successful registrations
	totalFilteredOut := 0                      // Count filtered-out tools

	// Map to store the tools by server name
	toolsByServer := make(map[string][]ToolData)

	// For each server, get tools and prepare them for registration
	for _, serverName := range serverNames {
		// Get server config for filtering
		serverConfig := mcpServers[serverName]

		transport, err := CreateTransport(serverConfig)
		if err != nil {
			return nil, err
		}

		clientSession, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return nil, err
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

		// Process each filtered tool
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

			// Convert InputSchema from mcp jsonschema (map[string]interface{}) to gai jsonschema (*jsonschema.Schema)
			inputSchemaJSON, err := json.Marshal(mcpTool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal input schema for tool %s: %w", mcpTool.Name, err)
			}

			var inputSchema *jsonschema.Schema
			if err := json.Unmarshal(inputSchemaJSON, &inputSchema); err != nil {
				return nil, fmt.Errorf("failed to unmarshal input schema for tool %s: %w", mcpTool.Name, err)
			}

			gaiTool := gai.Tool{
				Name:        mcpTool.Name,
				Description: mcpTool.Description,
				InputSchema: inputSchema,
			}

			// Store the tool data in the server's slice
			toolsByServer[serverName] = append(toolsByServer[serverName], ToolData{
				Tool:         gaiTool,
				MCPTool:      mcpTool,
				ToolCallback: callback,
			})

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
			return toolsByServer, nil
		}
		return toolsByServer, fmt.Errorf("failed to register any MCP tools: %s", strings.Join(warnings, "; "))
	}

	return toolsByServer, nil
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
