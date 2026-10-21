package mcpclient

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

const httpsScheme = "https"

// ServerID identifies a server in one session's catalog.
type ServerID string

// Connector opens a session-owned connection and registers toolsChanged for list notifications.
// ctx bounds connection setup; toolsChanged must not block.
type Connector func(ctx context.Context, server plugins.MCPServer, toolsChanged func()) (*mcp.ClientSession, error)

// Connect opens a stdio, streamable HTTP, or SSE connection using the official SDK.
// toolsChanged must be non-nil and nonblocking. Closing the returned session
// releases its transport and subprocess.
func Connect(ctx context.Context, server plugins.MCPServer, toolsChanged func()) (*mcp.ClientSession, error) {
	if toolsChanged == nil {
		return nil, errors.New("MCP tool-list change handler is required")
	}
	var transport mcp.Transport
	switch server.Type {
	case plugins.Stdio:
		if server.Command == "" {
			return nil, errors.New("MCP command is empty")
		}
		// The SDK owns process termination; setup cancellation must not kill a
		// successfully connected server when the NewSession request returns.
		command := exec.CommandContext(
			context.WithoutCancel(ctx),
			server.Command,
			server.Args...,
		)
		command.Dir = server.Cwd
		command.Env = os.Environ()
		for key, value := range server.Env {
			command.Env = append(
				command.Env,
				key+"="+value,
			)
		}
		transport = &mcp.CommandTransport{
			Command: command,
		}
	case plugins.StreamableHTTP, plugins.SSE:
		if server.URL.Host == "" || (server.URL.Scheme != "http" && server.URL.Scheme != httpsScheme) {
			return nil, errors.New("MCP URL must be absolute HTTP(S)")
		}
		client := &http.Client{
			Transport: headerTransport{
				base:    http.DefaultTransport,
				headers: server.Headers.Clone(),
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		if server.Type == plugins.SSE {
			transport = &mcp.SSEClientTransport{
				Endpoint:   server.URL.String(),
				HTTPClient: client,
			}
		} else {
			transport = &mcp.StreamableClientTransport{
				Endpoint:   server.URL.String(),
				HTTPClient: client,
			}
		}
	default:
		return nil, errors.New("unsupported MCP transport")
	}
	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "cpe",
			Version: "1",
		},
		&mcp.ClientOptions{
			ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { toolsChanged() },
		},
	)
	return client.Connect(
		ctx,
		transport,
		nil,
	)
}

type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	for key, values := range t.headers {
		copy.Header[key] = append(
			[]string(nil),
			values...,
		)
	}
	return t.base.RoundTrip(copy)
}
