package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xacp"
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

const (
	defaultServerTimeout = 60 * time.Second
	pdfMIMEType          = "application/pdf"
)

// effectiveServerType returns the runtime transport type, defaulting empty to stdio.
func effectiveServerType(config mcpconfig.ServerConfig) string {
	if config.Type == "" {
		return "stdio"
	}
	return config.Type
}

// effectiveServerTimeout returns the per-server operation timeout, defaulting to 60s.
func effectiveServerTimeout(config mcpconfig.ServerConfig) time.Duration {
	if config.Timeout <= 0 {
		return defaultServerTimeout
	}
	return time.Duration(config.Timeout) * time.Second
}

// withServerTimeout derives an operation-scoped timeout context from ctx.
func withServerTimeout(ctx context.Context, config mcpconfig.ServerConfig) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, effectiveServerTimeout(config))
}

// filterMcpTools applies per-server enabledTools/disabledTools policy.
// Mode is inferred from config: enabledTools (allowlist), disabledTools (blocklist),
// or pass-through when neither list is set. Input order is preserved.
//
// Returns the kept tools and names filtered out for observability logging.
func filterMcpTools(tools []*mcp.Tool, config mcpconfig.ServerConfig) ([]*mcp.Tool, []string) {
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

func mcpAnnotationsToACP(annotations *mcp.Annotations) *acp.Annotations {
	if annotations == nil {
		return nil
	}
	converted := &acp.Annotations{}
	if len(annotations.Audience) > 0 {
		audience := make([]acp.Role, len(annotations.Audience))
		for i, role := range annotations.Audience {
			audience[i] = acp.Role(role)
		}
		converted.Audience = &audience
	}
	if annotations.LastModified != "" {
		converted.LastModified = new(annotations.LastModified)
	}
	if annotations.Priority != 0 {
		converted.Priority = new(annotations.Priority)
	}
	return converted
}

// toolCallback adapts one MCP tool into gai.toolCallback invocation semantics.
// It is bound to a specific server session and tool name.
type toolCallback struct {
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
func (c *toolCallback) Call(ctx context.Context, parameters map[string]any) (gai.Message, error) {
	// Call the tool
	callCtx, cancel := withServerTimeout(ctx, c.ServerConfig)
	defer cancel()

	started := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
	started.Kind = new(acp.ToolKindOther)
	started.Status = new(acp.ToolCallStatusInProgress)
	started.RawInput = parameters
	if err := c.Conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: c.SessionId,
		Update:    started,
	}); err != nil {
		return gai.Message{}, fmt.Errorf("send in-progress tool call update: %w", err)
	}

	failedUpdate := func(text string) error {
		failed := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
		failed.Status = new(acp.ToolCallStatusFailed)
		failed.Content = []acp.ToolCallContent{acp.ContentToolCallContent(acp.TextContentBlock(text))}
		if err := c.Conn.SessionUpdate(ctx, &acp.SessionNotification{
			SessionID: c.SessionId,
			Update:    failed,
		}); err != nil {
			return fmt.Errorf("send failed tool call update: %w", err)
		}
		return nil
	}

	result, err := c.ClientSession.CallTool(callCtx, &mcp.CallToolParams{
		Name:      c.ToolName,
		Arguments: parameters,
	})
	if err != nil {
		errText := fmt.Sprintf("Error calling MCP tool %s/%s: %v", c.ServerName, c.ToolName, err)
		if updateErr := failedUpdate(errText); updateErr != nil {
			return gai.Message{}, updateErr
		}
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
	acpContentOverrides := make(map[int]acp.ToolCallContent)
	for _, content := range result.Content {
		var block gai.Block

		switch c := content.(type) {
		case *mcp.TextContent:
			block = gai.TextBlock(c.Text)
		case *mcp.ImageContent:
			// ImageContent.Data contains raw bytes (already base64-decoded by json.Unmarshal).
			if c.MIMEType == pdfMIMEType || c.MIMEType == "application/x-pdf" {
				block = gai.PDFBlock(c.Data, "document.pdf")
			} else {
				block = gai.ImageBlock(c.Data, c.MIMEType)
			}
		case *mcp.AudioContent:
			block = gai.AudioBlock(c.Data, c.MIMEType)
		case *mcp.ResourceLink:
			text := fmt.Sprintf("Resource link: %s (%s)", c.Name, c.URI)
			if c.Title != "" {
				text += "\nTitle: " + c.Title
			}
			if c.Description != "" {
				text += "\nDescription: " + c.Description
			}
			block = gai.TextBlock(text)

			resourceLink := acp.ResourceLinkContentBlock(c.Name, c.URI)
			resourceLink.Meta = acp.Meta(c.Meta)
			resourceLink.Annotations = mcpAnnotationsToACP(c.Annotations)
			if c.Title != "" {
				resourceLink.Title = new(c.Title)
			}
			if c.Description != "" {
				resourceLink.Description = new(c.Description)
			}
			if c.MIMEType != "" {
				resourceLink.MimeType = new(c.MIMEType)
			}
			resourceLink.Size = c.Size
			acpContentOverrides[len(blocks)] = acp.ContentToolCallContent(resourceLink)
		case *mcp.EmbeddedResource:
			if c.Resource == nil {
				errText := "embedded resource is missing resource contents"
				if updateErr := failedUpdate(errText); updateErr != nil {
					return gai.Message{}, updateErr
				}
				return gai.Message{}, fmt.Errorf("%s", errText)
			}
			toACPContent := func(resource acp.EmbeddedResourceResource) acp.ToolCallContent {
				resource.Meta = acp.Meta(c.Resource.Meta)
				contentBlock := acp.ResourceContentBlock(resource)
				contentBlock.Meta = acp.Meta(c.Meta)
				contentBlock.Annotations = mcpAnnotationsToACP(c.Annotations)
				return acp.ContentToolCallContent(contentBlock)
			}
			if c.Resource.Blob != nil {
				encoded := base64.StdEncoding.EncodeToString(c.Resource.Blob)
				switch {
				case c.Resource.MIMEType == pdfMIMEType || c.Resource.MIMEType == "application/x-pdf":
					filename := "document.pdf"
					if uri, err := url.Parse(c.Resource.URI); err == nil {
						if base := path.Base(uri.Path); base != "" && base != "." && base != "/" {
							filename = base
						}
					}
					block = gai.PDFBlock(c.Resource.Blob, filename)
				case strings.HasPrefix(c.Resource.MIMEType, "image/"):
					block = gai.ImageBlock(c.Resource.Blob, c.Resource.MIMEType)
				case strings.HasPrefix(c.Resource.MIMEType, "audio/"):
					block = gai.AudioBlock(c.Resource.Blob, c.Resource.MIMEType)
				case strings.HasPrefix(c.Resource.MIMEType, "video/"):
					block = gai.Block{
						BlockType:    gai.Content,
						ModalityType: gai.Video,
						MimeType:     c.Resource.MIMEType,
						Content:      gai.Str(encoded),
					}
				default:
					block = gai.TextBlock(fmt.Sprintf(
						"Embedded resource: %s (%s, %d bytes)",
						c.Resource.URI,
						c.Resource.MIMEType,
						len(c.Resource.Blob),
					))
				}

				resource := acp.BlobResourceContentsEmbeddedResourceResource(encoded, c.Resource.URI)
				if c.Resource.MIMEType != "" {
					resource.MimeType = new(c.Resource.MIMEType)
				}
				acpContentOverrides[len(blocks)] = toACPContent(resource)
				break
			}

			block = gai.TextBlock(c.Resource.Text)
			resource := acp.TextResourceContentsEmbeddedResourceResource(c.Resource.Text, c.Resource.URI)
			if c.Resource.MIMEType != "" {
				resource.MimeType = new(c.Resource.MIMEType)
			}
			acpContentOverrides[len(blocks)] = toACPContent(resource)
		default:
			errText := fmt.Sprintf("cannot handle tool call result content type %T", content)
			if updateErr := failedUpdate(errText); updateErr != nil {
				return gai.Message{}, updateErr
			}
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

	acpBlocks := xacp.BlocksToToolCallContent(blocks)
	for i, override := range acpContentOverrides {
		acpBlocks[i] = override
	}

	completed := acp.ToolCallUpdateSessionUpdate(xctx.ToolCallIdFrom(ctx))
	completed.Status = new(status)
	completed.Content = acpBlocks
	if err := c.Conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: c.SessionId,
		Update:    completed,
	}); err != nil {
		return gai.Message{}, fmt.Errorf("send %s tool call update: %w", status, err)
	}

	return resultMsg, nil
}

// createTransport builds the transport used during client.Connect.
//
// - stdio: spawns the configured command, forwards stderr, and injects configured env
// - http/sse: builds endpoint transports with optional request headers
//
// Session lifecycle (connect/close) is managed by callers after transport creation.
func createTransport(ctx context.Context, config mcpconfig.ServerConfig) (transport mcp.Transport, err error) {
	serverType := effectiveServerType(config)

	// Create a custom HTTP client only for static header injection.
	// Per-operation timeouts are enforced via context deadlines so long-lived
	// HTTP/SSE sessions are not terminated by http.Client.Timeout.
	var httpClient *http.Client
	if serverType == "http" || serverType == "sse" {
		transport := httpclient.Transport(
			httpclient.WithBaseTransport(nil),
			httpclient.WithRetryStatuses(false),
			httpclient.WithBackoff(200*time.Millisecond, 5*time.Second),
			httpclient.WithJitterFactor(0.2),
			httpclient.WithMaxRetries(2),
		)
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

// newClient constructs the MCP client identity announced during initialize.
// Version is sourced from the running CPE build metadata.
func newClient() *mcp.Client {
	return mcp.NewClient(
		&mcp.Implementation{
			Name:    "cpe",
			Title:   "CPE",
			Version: version.Get(),
		}, nil,
	)
}
