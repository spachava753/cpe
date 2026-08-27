package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

// connectServer establishes one MCP connection using the configured transport.
// It does not list tools. The returned connection must be closed by the caller.
func connectServer(ctx context.Context, serverName string, config mcpconfig.ServerConfig) (*mcpConn, error) {
	client := newClient()
	return connectServerSession(ctx, client, serverName, config)
}

// InitializeConnections establishes sessions to all configured MCP servers,
// lists tools, applies per-server filtering, and validates cross-server tool name
// uniqueness after filtering.
//
// It fails fast: on any connect/list/validation error, already-open sessions are
// closed before returning.
func InitializeConnections(
	ctx context.Context,
	servers map[string]mcpconfig.ServerConfig,
) (*MCPState, error) {
	if len(servers) == 0 {
		return NewMCPState(), nil
	}

	// Sort server names for deterministic connection order and error messages
	serverNames := make([]string, 0, len(servers))
	for name := range servers {
		serverNames = append(serverNames, name)
	}
	slices.Sort(serverNames)

	client := newClient()
	state := NewMCPState()

	// Track tool names for duplicate detection
	toolOwners := make(map[string]string) // tool name -> server name

	for _, serverName := range serverNames {
		serverConfig := servers[serverName]
		conn, err := connectToServer(ctx, client, serverName, serverConfig)
		if err != nil {
			// Fail fast: close any connections we've made so far
			state.Close()
			return nil, fmt.Errorf("server %s: %w", serverName, err)
		}

		// Check for duplicate tool names
		for _, tool := range conn.Tools {
			if existingServer, exists := toolOwners[tool.Name]; exists {
				_ = conn.ClientSession.Close()
				if conn.close != nil {
					_ = conn.close()
				}
				state.Close()
				return nil, fmt.Errorf("duplicate tool name %q: found in both %q and %q",
					tool.Name, existingServer, serverName)
			}
			toolOwners[tool.Name] = serverName
		}

		state.Connections[serverName] = conn
	}

	slog.InfoContext(ctx, "MCP connections initialized", "servers", len(state.Connections))
	return state, nil
}

// connectToServer creates one connection, fetches tools, and applies
// enabled/disabled filtering for that server.
func connectToServer(
	ctx context.Context,
	client *mcpsdk.Client,
	serverName string,
	config mcpconfig.ServerConfig,
) (*mcpConn, error) {
	conn, err := connectServerSession(ctx, client, serverName, config)
	if err != nil {
		return nil, err
	}
	if err := populateConnectionTools(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func connectServerSession(
	ctx context.Context,
	client *mcpsdk.Client,
	serverName string,
	config mcpconfig.ServerConfig,
) (*mcpConn, error) {
	transport, err := createTransport(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("creating transport: %w", err)
	}

	operationCtx, cancel := withServerTimeout(ctx, config)
	defer cancel()

	session, err := client.Connect(operationCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}

	return &mcpConn{
		ServerName:    serverName,
		Config:        config,
		ClientSession: session,
	}, nil
}

func populateConnectionTools(ctx context.Context, conn *mcpConn) error {
	operationCtx, cancel := withServerTimeout(ctx, conn.Config)
	defer cancel()

	var allTools []*mcpsdk.Tool
	for tool, err := range conn.ClientSession.Tools(operationCtx, nil) {
		if err != nil {
			return fmt.Errorf("listing tools: %w", err)
		}
		allTools = append(allTools, tool)
	}

	filteredTools, filteredOut := filterMcpTools(allTools, conn.Config)
	if len(filteredOut) > 0 {
		slog.InfoContext(ctx, "MCP tools filtered",
			"server", conn.ServerName,
			"filtered_count", len(filteredOut),
			"filtered", strings.Join(filteredOut, ", "))
	}

	conn.Tools = filteredTools
	conn.AllTools = allTools
	conn.FilteredOut = filteredOut
	return nil
}
