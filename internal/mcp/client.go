package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/version"
)

// subagentLoggingAddressEnv mirrors subagentlog.SubagentLoggingAddressEnv.
// Defined locally to avoid import cycle (subagentlog imports agent which imports mcp).
const subagentLoggingAddressEnv = "CPE_SUBAGENT_LOGGING_ADDRESS"

// headerRoundTripper injects configured static headers into each outgoing request.
type headerRoundTripper struct {
	headers map[string]string
	next    http.RoundTripper
}

// RoundTrip applies headers then delegates to the wrapped transport.
func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for name, value := range h.headers {
		req.Header.Set(name, value)
	}
	if h.next == nil {
		h.next = http.DefaultTransport
	}
	return h.next.RoundTrip(req)
}

const defaultServerTimeout = 60 * time.Second

// EffectiveServerType returns the runtime transport type, defaulting empty to stdio.
func EffectiveServerType(config mcpconfig.ServerConfig) string {
	if config.Type == "" {
		return "stdio"
	}
	return config.Type
}

// EffectiveServerTimeout returns the per-server operation timeout, defaulting to 60s.
func EffectiveServerTimeout(config mcpconfig.ServerConfig) time.Duration {
	if config.Timeout <= 0 {
		return defaultServerTimeout
	}
	return time.Duration(config.Timeout) * time.Second
}

// WithServerTimeout derives an operation-scoped timeout context from ctx.
func WithServerTimeout(ctx context.Context, config mcpconfig.ServerConfig) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, EffectiveServerTimeout(config))
}

// FilterMcpTools applies per-server enabledTools/disabledTools policy.
// Mode is inferred from config: enabledTools (allowlist), disabledTools (blocklist),
// or pass-through when neither list is set. Input order is preserved.
//
// Returns the kept tools and names filtered out for observability logging.
func FilterMcpTools(tools []*mcp.Tool, config mcpconfig.ServerConfig) ([]*mcp.Tool, []string) {
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

// ToolCallback adapts one MCP tool into gai.ToolCallback invocation semantics.
// It is bound to a specific server session and tool name.
type ToolCallback struct {
	ClientSession *mcp.ClientSession
	ToolName      string
	ServerName    string
	ServerConfig  mcpconfig.ServerConfig
}

// Call executes the bound MCP tool and converts MCP content into gai blocks.
// Parameter/tool-call failures are returned as ToolResult text (nil error) so the
// model can recover; unsupported content types return a hard error.
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
	callCtx, cancel := WithServerTimeout(ctx, c.ServerConfig)
	defer cancel()

	result, err := c.ClientSession.CallTool(callCtx, &mcp.CallToolParams{
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
	blocks := make([]gai.Block, 0, len(result.Content))
	for _, content := range result.Content {
		var block gai.Block

		switch c := content.(type) {
		case *mcp.TextContent:
			block = gai.TextBlock(c.Text)
		case *mcp.ImageContent:
			// ImageContent.Data contains raw bytes (already base64-decoded by json.Unmarshal).
			if c.MIMEType == "application/pdf" || c.MIMEType == "application/x-pdf" {
				block = gai.PDFBlock(c.Data, "document.pdf")
			} else {
				block = gai.ImageBlock(c.Data, c.MIMEType)
			}
		case *mcp.AudioContent:
			block = gai.AudioBlock(c.Data, c.MIMEType)
		case *mcp.ResourceLink:
			return gai.Message{}, fmt.Errorf("cannot handle resource links in tool call result")
		case *mcp.EmbeddedResource:
			return gai.Message{}, fmt.Errorf("cannot handle embedded resources in tool call result")
		default:
			return gai.Message{}, fmt.Errorf("cannot handle tool call result content type %T", content)
		}

		block.ID = toolCallID
		blocks = append(blocks, block)
	}

	return gai.Message{
		Role:   gai.ToolResult,
		Blocks: blocks,
	}, nil
}

// CreateTransport builds the transport used during client.Connect.
//
// - stdio: spawns the configured command, forwards stderr, and injects env/logging hooks
// - http/sse: builds endpoint transports with optional request headers
//
// Session lifecycle (connect/close) is managed by callers after transport creation.
func CreateTransport(ctx context.Context, config mcpconfig.ServerConfig, loggingAddress string) (transport mcp.Transport, err error) {
	serverType := EffectiveServerType(config)

	// Create a custom HTTP client only for static header injection.
	// Per-operation timeouts are enforced via context deadlines so long-lived
	// HTTP/SSE sessions are not terminated by http.Client.Timeout.
	var httpClient *http.Client
	if serverType == "http" || serverType == "sse" {
		httpClient = &http.Client{}
		if len(config.Headers) > 0 {
			httpClient.Transport = &headerRoundTripper{headers: config.Headers}
		}
	}

	switch serverType {
	case "stdio":
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

// NewClient constructs the MCP client identity announced during initialize.
// Version is sourced from the running CPE build metadata.
func NewClient() *mcp.Client {
	return mcp.NewClient(
		&mcp.Implementation{
			Name:    "cpe",
			Title:   "CPE",
			Version: version.Get(),
		}, nil,
	)
}
