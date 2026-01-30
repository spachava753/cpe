package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/version"
)

// subagentLoggingAddressEnv mirrors subagentlog.SubagentLoggingAddressEnv.
// Defined locally to avoid import cycle (subagentlog imports agent which imports mcp).
const subagentLoggingAddressEnv = "CPE_SUBAGENT_LOGGING_ADDRESS"

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
	EnabledTools  []string          `json:"enabledTools,omitempty" yaml:"enabledTools,omitempty" validate:"omitempty,min=1,excluded_with=DisabledTools"`
	DisabledTools []string          `json:"disabledTools,omitempty" yaml:"disabledTools,omitempty" validate:"omitempty,min=1,excluded_with=EnabledTools"`
}

// FilterMcpTools applies tool filtering based on the server configuration.
// The filtering mode is inferred from which list is populated:
//   - If EnabledTools is non-empty: whitelist mode (only those tools)
//   - If DisabledTools is non-empty: blacklist mode (exclude those tools)
//   - If both are empty: allow all tools
//
// Returns the filtered tools and a list of filtered-out tool names for logging.
func FilterMcpTools(tools []*mcp.Tool, config ServerConfig) ([]*mcp.Tool, []string) {
	// Infer filtering mode from which list is populated
	if len(config.EnabledTools) > 0 {
		// Whitelist mode: only include tools in EnabledTools
		enabledSet := make(map[string]bool)
		for _, toolName := range config.EnabledTools {
			enabledSet[toolName] = true
		}

		var filteredTools []*mcp.Tool
		var filteredOut []string
		for _, tool := range tools {
			if enabledSet[tool.Name] {
				filteredTools = append(filteredTools, tool)
			} else {
				filteredOut = append(filteredOut, tool.Name)
			}
		}
		return filteredTools, filteredOut
	}

	if len(config.DisabledTools) > 0 {
		// Blacklist mode: exclude tools in DisabledTools
		disabledSet := make(map[string]bool)
		for _, toolName := range config.DisabledTools {
			disabledSet[toolName] = true
		}

		var filteredTools []*mcp.Tool
		var filteredOut []string
		for _, tool := range tools {
			if !disabledSet[tool.Name] {
				filteredTools = append(filteredTools, tool)
			} else {
				filteredOut = append(filteredOut, tool.Name)
			}
		}
		return filteredTools, filteredOut
	}

	// No filtering: return all tools
	return tools, nil
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

func CreateTransport(ctx context.Context, config ServerConfig, loggingAddress string) (transport mcp.Transport, err error) {
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
		cmd := exec.CommandContext(ctx, config.Command, config.Args...)
		// Forward stderr to parent so subagent debug/event output is visible
		cmd.Stderr = os.Stderr
		// Always set cmd.Env to ensure we control the environment
		cmd.Env = os.Environ()
		// Add custom environment variables from config
		for k, v := range config.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		// Inject subagent logging address
		if loggingAddress != "" {
			cmd.Env = append(cmd.Env, subagentLoggingAddressEnv+"="+loggingAddress)
		}
		transport = &mcp.CommandTransport{
			Command: cmd,
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

func NewClient() *mcp.Client {
	return mcp.NewClient(
		&mcp.Implementation{
			Name:    "cpe",
			Title:   "CPE",
			Version: version.Get(),
		}, nil,
	)
}
