package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

// Registry owns one session's MCP connections and cached tool lists.
// Its methods serialize calls, discovery, and shutdown.
type Registry struct {
	connector   Connector
	servers     map[ServerID]plugins.MCPServer
	connections map[ServerID]*mcp.ClientSession
	tools       map[ServerID]*toolList
	mu          sync.Mutex
	closed      bool
}

// Notification callbacks only advance revision: they must never wait on the registry
// mutex while an MCP request or connection shutdown holds it.
type toolList struct {
	tools          []*mcp.Tool
	revision       atomic.Uint64
	loadedRevision uint64
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
	list := m.tools[id]
	connection, err := m.connector(
		ctx,
		server,
		func() { list.revision.Add(1) },
	)
	if err != nil {
		return nil, err
	}
	m.connections[id] = connection
	return connection, nil
}

// Load returns a cached tool list, refreshing it after tool-list change notifications.
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
	list := m.tools[id]
	revision := list.revision.Load()
	if list.loadedRevision == revision {
		return list.tools, nil
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
	// Keep the revision captured before discovery so a concurrent notification
	// still triggers another refresh on the next Load. Failed loads never cache.
	list.tools = tools
	list.loadedRevision = revision
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
	tools := make(map[ServerID]*toolList)
	for id := range servers {
		list := &toolList{}
		list.revision.Store(1)
		tools[id] = list
	}
	return &Registry{
		connector:   connector,
		servers:     servers,
		connections: make(map[ServerID]*mcp.ClientSession),
		tools:       tools,
	}, nil
}
