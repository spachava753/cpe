package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

const httpsScheme = "https"

// MCPServerID identifies a server in one session's catalog.
type MCPServerID string

type mcpServer = plugins.MCPServer

// MCPConnector returns a session-owned SDK connection. ctx bounds connection setup.
type MCPConnector func(context.Context, plugins.MCPServer) (*mcp.ClientSession, error)

// ConnectMCP opens a stdio, streamable HTTP, or SSE connection using the official SDK.
// Closing the returned session releases its transport and subprocess.
func ConnectMCP(ctx context.Context, server plugins.MCPServer) (*mcp.ClientSession, error) {
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
		nil,
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

// MCPTools retains SDK results for scripts; conversion to model content is separate.
type MCPTools interface {
	Servers() []MCPServerID
	Load(context.Context, MCPServerID) ([]*mcp.Tool, error)
	Call(context.Context, MCPServerID, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

// Session execution is serial; the mutex also coordinates setup and shutdown.
type mcpRegistry struct {
	mu          sync.Mutex
	closed      bool
	connector   MCPConnector
	servers     map[MCPServerID]mcpServer
	connections map[MCPServerID]*mcp.ClientSession
}

func (m *mcpRegistry) Servers() []MCPServerID {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make(
		[]MCPServerID,
		0,
		len(m.servers),
	)
	for id := range m.servers {
		ids = append(
			ids,
			id,
		)
	}
	slices.Sort(ids)
	return ids
}

func (m *mcpRegistry) connect(ctx context.Context, id MCPServerID) (*mcp.ClientSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.closed {
		return nil, errors.New("MCP registry is closed")
	}
	if connection := m.connections[id]; connection != nil {
		return connection, nil
	}
	server, ok := m.servers[id]
	if !ok {
		return nil, fmt.Errorf(
			"unknown MCP server %q",
			id,
		)
	}
	connection, err := m.connector(
		ctx,
		server,
	)
	if err != nil {
		return nil, err
	}
	m.connections[id] = connection
	return connection, nil
}

func (m *mcpRegistry) Load(ctx context.Context, id MCPServerID) ([]*mcp.Tool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	connection, err := m.connect(
		ctx,
		id,
	)
	if err != nil {
		return nil, err
	}
	var tools []*mcp.Tool
	for tool, err := range connection.Tools(
		ctx,
		nil,
	) {
		if err != nil {
			return nil, err
		}
		tools = append(
			tools,
			tool,
		)
	}
	return tools, nil
}

func (m *mcpRegistry) Call(ctx context.Context, id MCPServerID, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if params == nil || params.Name == "" {
		return nil, errors.New("MCP tool name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	connection, err := m.connect(
		ctx,
		id,
	)
	if err != nil {
		return nil, err
	}
	return connection.CallTool(
		ctx,
		params,
	)
}

func (m *mcpRegistry) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	var errs []error
	for id, connection := range m.connections {
		errs = append(
			errs,
			connection.Close(),
		)
		delete(
			m.connections,
			id,
		)
	}
	return errors.Join(errs...)
}
