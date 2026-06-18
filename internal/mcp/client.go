package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/httpclient"
	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/version"
)

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

func newMCPRoundTripper(base http.RoundTripper) http.RoundTripper {
	return httpclient.Transport(
		httpclient.WithBaseTransport(base),
		httpclient.WithRetryStatuses(false),
		httpclient.WithBackoff(200*time.Millisecond, 5*time.Second),
		httpclient.WithJitterFactor(0.2),
		httpclient.WithMaxRetries(2),
	)
}

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

type sessionUpdator interface {
	SessionUpdate(ctx context.Context, params *acp.SessionNotification) error
}

// ToolCallback adapts one MCP tool into gai.ToolCallback invocation semantics.
// It is bound to a specific server session and tool name.
type ToolCallback struct {
	Conn          sessionUpdator
	SessionId     acp.SessionId
	ClientSession *mcp.ClientSession
	ToolName      string
	ServerName    string
	ServerConfig  mcpconfig.ServerConfig
}

// Call executes the bound MCP tool and converts MCP content into gai blocks.
// Parameter/tool-call failures are returned as ToolResult text (nil error) so the
// model can recover; unsupported content types return a hard error.
func (c *ToolCallback) Call(ctx context.Context, parameters map[string]any) (gai.Message, error) {
	// Call the tool
	callCtx, cancel := WithServerTimeout(ctx, c.ServerConfig)
	defer cancel()

	started := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
	started.Kind = new(acp.ToolKindOther)
	started.Status = new(acp.ToolCallStatusInProgress)
	started.RawInput = parameters
	_ = c.Conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: c.SessionId,
		Update:    started,
	})

	failedUpdate := func(text string) {
		failed := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
		failed.Status = new(acp.ToolCallStatusFailed)
		failed.Content = []acp.ToolCallContent{acp.ContentToolCallContent(acp.TextContentBlock(text))}
		_ = c.Conn.SessionUpdate(ctx, &acp.SessionNotification{
			SessionID: c.SessionId,
			Update:    failed,
		})
	}

	result, err := c.ClientSession.CallTool(callCtx, &mcp.CallToolParams{
		Name:      c.ToolName,
		Arguments: parameters,
	})
	if err != nil {
		errText := fmt.Sprintf("Error calling MCP tool %s/%s: %v", c.ServerName, c.ToolName, err)
		failedUpdate(errText)
		return gai.Message{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str(errText),
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
			errText := "cannot handle resource links in tool call result"
			failedUpdate(errText)
			return gai.Message{}, fmt.Errorf("%s", errText)
		case *mcp.EmbeddedResource:
			errText := "cannot handle embedded resources in tool call result"
			failedUpdate(errText)
			return gai.Message{}, fmt.Errorf("%s", errText)
		default:
			errText := fmt.Sprintf("cannot handle tool call result content type %T", content)
			failedUpdate(errText)
			return gai.Message{}, fmt.Errorf("%s", errText)
		}

		blocks = append(blocks, block)
	}

	resultMsg := gai.Message{
		Role:            gai.ToolResult,
		Blocks:          blocks,
		ToolResultError: result.IsError,
	}

	status := acp.ToolCallStatusCompleted
	if result.IsError {
		status = acp.ToolCallStatusFailed
	}

	acpBlocks := make([]acp.ToolCallContent, len(blocks))
	for i, b := range blocks {
		var contentBlock acp.ContentBlock
		content := b.Content.String()
		switch b.ModalityType {
		case gai.Image:
			contentBlock = acp.ImageContentBlock(content, b.MimeType)
		case gai.Audio:
			contentBlock = acp.AudioContentBlock(content, b.MimeType)
		default:
			contentBlock = acp.TextContentBlock(content)
		}
		acpBlocks[i] = acp.ContentToolCallContent(contentBlock)
	}

	completed := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
	completed.Status = new(status)
	completed.Content = acpBlocks
	_ = c.Conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: c.SessionId,
		Update:    completed,
	})

	return resultMsg, nil
}

// CreateTransport builds the transport used during client.Connect.
//
// - stdio: spawns the configured command, forwards stderr, and injects configured env
// - http/sse: builds endpoint transports with optional request headers
//
// Session lifecycle (connect/close) is managed by callers after transport creation.
func CreateTransport(ctx context.Context, config mcpconfig.ServerConfig) (transport mcp.Transport, err error) {
	serverType := EffectiveServerType(config)

	// Create a custom HTTP client only for static header injection.
	// Per-operation timeouts are enforced via context deadlines so long-lived
	// HTTP/SSE sessions are not terminated by http.Client.Timeout.
	var httpClient *http.Client
	if serverType == "http" || serverType == "sse" {
		transport := newMCPRoundTripper(nil)
		if len(config.Headers) > 0 {
			transport = &headerRoundTripper{headers: config.Headers, next: transport}
		}
		httpClient = &http.Client{Transport: transport}
	}

	switch serverType {
	case "stdio":
		cmd := exec.CommandContext(ctx, config.Command, config.Args...)
		// Forward stderr so server diagnostics remain visible.
		cmd.Stderr = os.Stderr
		// Always set cmd.Env to ensure we control the environment
		cmd.Env = os.Environ()
		// Add custom environment variables from config
		for k, v := range config.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
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
