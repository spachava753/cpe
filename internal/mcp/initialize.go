package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// InitializeConnections connects to all configured MCP servers and fetches their tools.
// It applies tool filtering per server configuration (enabledTools/disabledTools).
// Returns an MCPState that must be closed when done.
//
// Fails fast: returns error immediately if any server fails to connect or if
// duplicate tool names are detected across servers.
func InitializeConnections(
	ctx context.Context,
	servers map[string]ServerConfig,
	loggingAddress string,
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

	client := NewClient()
	state := NewMCPState()

	// Track tool names for duplicate detection
	toolOwners := make(map[string]string) // tool name -> server name

	for _, serverName := range serverNames {
		serverConfig := servers[serverName]
		conn, err := connectToServer(ctx, client, serverName, serverConfig, loggingAddress)
		if err != nil {
			// Fail fast: close any connections we've made so far
			state.Close()
			return nil, fmt.Errorf("server %s: %w", serverName, err)
		}

		// Check for duplicate tool names
		for _, tool := range conn.Tools {
			if existingServer, exists := toolOwners[tool.Name]; exists {
				state.Close()
				return nil, fmt.Errorf("duplicate tool name %q: found in both %q and %q",
					tool.Name, existingServer, serverName)
			}
			toolOwners[tool.Name] = serverName
		}

		state.Connections[serverName] = conn
	}

	slog.Info("MCP connections initialized", "servers", len(state.Connections))
	return state, nil
}

func connectToServer(
	ctx context.Context,
	client *mcpsdk.Client,
	serverName string,
	config ServerConfig,
	loggingAddress string,
) (*MCPConn, error) {
	transport, err := CreateTransport(ctx, config, loggingAddress)
	if err != nil {
		return nil, fmt.Errorf("creating transport: %w", err)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}

	// Fetch tools
	var allTools []*mcpsdk.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			session.Close()
			return nil, fmt.Errorf("listing tools: %w", err)
		}
		allTools = append(allTools, tool)
	}

	// Apply filtering
	filteredTools, filteredOut := FilterMcpTools(allTools, config)
	if len(filteredOut) > 0 {
		slog.Info("MCP tools filtered",
			"server", serverName,
			"filtered_count", len(filteredOut),
			"filtered", strings.Join(filteredOut, ", "))
	}

	return &MCPConn{
		ServerName:    serverName,
		Config:        config,
		ClientSession: session,
		Tools:         filteredTools,
	}, nil
}
