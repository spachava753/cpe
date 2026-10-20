package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

// Registry owns one session's MCP connections and cached tool lists.
// Its methods serialize calls, discovery, and shutdown.
type Registry struct {
	mu          sync.Mutex
	closed      bool
	connector   Connector
	servers     map[ServerID]plugins.MCPServer
	connections map[ServerID]*mcp.ClientSession
	tools       map[ServerID][]*mcp.Tool
}

// Servers returns configured server IDs in sorted order.
func (m *Registry) Servers() []ServerID {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make(
		[]ServerID,
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

func (m *Registry) connect(ctx context.Context, id ServerID) (*mcp.ClientSession, error) {
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

// Load connects to a server and caches its tool list until Close.
func (m *Registry) Load(ctx context.Context, id ServerID) ([]*mcp.Tool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	connection, err := m.connect(
		ctx,
		id,
	)
	if err != nil {
		return nil, err
	}
	if tools, ok := m.tools[id]; ok {
		return tools, nil
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
	m.tools[id] = tools
	return tools, nil
}

// Instructions returns initialization guidance, or an empty string before connection.
func (m *Registry) Instructions(id ServerID) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if connection := m.connections[id]; connection != nil {
		return connection.InitializeResult().Instructions
	}
	return ""
}

// Call invokes a remote tool, retaining the SDK result and error status.
func (m *Registry) Call(ctx context.Context, id ServerID, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
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

// Close releases all opened connections and prevents further discovery or calls.
func (m *Registry) Close() error {
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

// NewRegistry creates a connection owner without opening any connections.
// Callers must not mutate servers or their configuration until Close returns.
func NewRegistry(connector Connector, servers map[ServerID]plugins.MCPServer) (*Registry, error) {
	if connector == nil {
		return nil, errors.New("MCP connector is required")
	}
	return &Registry{
		connector:   connector,
		servers:     servers,
		connections: make(map[ServerID]*mcp.ClientSession),
		tools:       make(map[ServerID][]*mcp.Tool),
	}, nil
}
