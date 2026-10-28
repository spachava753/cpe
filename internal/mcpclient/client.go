package mcpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/version"
)

const (
	httpScheme  = "http"
	httpsScheme = "https"
)

// ServerID identifies a server among one session's configured servers.
type ServerID string

// Connector opens a connection. ctx bounds connection setup only. Stdio servers
// write their stderr to stderr, which may be nil to discard it.
type Connector func(ctx context.Context, server plugins.MCPServer, stderr io.Writer) (*mcp.ClientSession, error)

// Connect opens a stdio, streamable HTTP, or SSE connection using the official SDK.
// Closing the returned session releases its transport and subprocess.
func Connect(ctx context.Context, server plugins.MCPServer, stderr io.Writer) (*mcp.ClientSession, error) {
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
		command.Stderr = stderr
		transport = &mcp.CommandTransport{
			Command: command,
		}
	case plugins.StreamableHTTP, plugins.SSE:
		if server.URL.Host == "" || (server.URL.Scheme != httpScheme && server.URL.Scheme != httpsScheme) {
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
	return newClient().Connect(
		ctx,
		transport,
		nil,
	)
}

func newClient() *mcp.Client {
	return mcp.NewClient(
		&mcp.Implementation{
			Name:    "cpe",
			Version: version.Get(),
		},
		&mcp.ClientOptions{
			// Session catalogs stay fixed, but subscribing lets the SDK drop its
			// cached tool list when a server announces a change, so the next
			// session to list a shared connection's tools sees the current list.
			ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {},
		},
	)
}

type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	for key, values := range t.headers {
		copy.Header[key] = slices.Clone(values)
	}
	return t.base.RoundTrip(copy)
}
