package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ClientManager manages connections to MCP servers
type ClientManager struct {
	clients map[string]*client.Client
	config  *ConfigFile
}

// NewClientManager creates a new ClientManager
func NewClientManager(config *ConfigFile) *ClientManager {
	// If the config is nil, create a default one with CPE's native tools
	if config == nil {
		config = &ConfigFile{MCPServers: make(map[string]MCPServerConfig)}
	}

	// If no servers are configured, add CPE's own serve command as an MCP server
	if len(config.MCPServers) == 0 {
		fmt.Fprintf(os.Stderr, "WARNING: No MCP servers configured.\n")
	}

	return &ClientManager{
		clients: make(map[string]*client.Client),
		config:  config,
	}
}

// GetClient returns an initialized client for the specified server
func (m *ClientManager) GetClient(ctx context.Context, serverName string) (*client.Client, error) {
	// Check if client already exists
	if c, ok := m.clients[serverName]; ok {
		return c, nil
	}

	// Get server config
	serverConfig, ok := m.config.MCPServers[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found in configuration", serverName)
	}

	// Create client based on type
	var c *client.Client
	var err error

	switch serverConfig.Type {
	case "", "stdio":
		// Use provided env or empty if not present
		env := make([]string, 0, len(serverConfig.Env))
		for k, v := range serverConfig.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}

		c, err = client.NewStdioMCPClient(
			serverConfig.Command,
			env,
			serverConfig.Args...,
		)
	case "sse":
		c, err = client.NewSSEMCPClient(serverConfig.URL)
	case "http":
		c, err = client.NewStreamableHttpClient(serverConfig.URL)
	default:
		return nil, fmt.Errorf("unsupported client type: %s", serverConfig.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create client for server %q: %w", serverName, err)
	}

	// start the client
	err = c.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start client for server %s: %w", serverName, err)
	}

	// Store the client for future use
	m.clients[serverName] = c

	return c, nil
}

// InitializeClient initializes a client with the MCP protocol
func (m *ClientManager) InitializeClient(ctx context.Context, serverName string) (*mcp.InitializeResult, error) {
	c, err := m.GetClient(ctx, serverName)
	if err != nil {
		return nil, err
	}

	// Get timeout setting
	serverConfig := m.config.MCPServers[serverName]
	timeoutSeconds := 60 // Default timeout
	if serverConfig.Timeout > 0 {
		timeoutSeconds = serverConfig.Timeout
	}

	// Initialize the client
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "cpe",
		Version: "1.0.0", //TODO: get the bundled cpe version from debug info like we do with the version flag and use that here
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	initResult, err := c.Initialize(ctx, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	return initResult, nil
}

// ListTools lists the available tools from an MCP server
func (m *ClientManager) ListTools(ctx context.Context, serverName string) (*mcp.ListToolsResult, error) {
	c, err := m.GetClient(ctx, serverName)
	if err != nil {
		return nil, err
	}

	// Get timeout setting
	serverConfig := m.config.MCPServers[serverName]
	timeoutSeconds := 60 // Default timeout
	if serverConfig.Timeout > 0 {
		timeoutSeconds = serverConfig.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	toolsRequest := mcp.ListToolsRequest{}
	tools, err := c.ListTools(ctx, toolsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	return tools, nil
}

// CallTool calls a tool on an MCP server
func (m *ClientManager) CallTool(ctx context.Context, serverName string, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	c, err := m.GetClient(ctx, serverName)
	if err != nil {
		return nil, err
	}

	// Get timeout setting
	serverConfig := m.config.MCPServers[serverName]
	timeoutSeconds := 60 // Default timeout
	if serverConfig.Timeout > 0 {
		timeoutSeconds = serverConfig.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = toolName
	if args != nil {
		callRequest.Params.Arguments = args
	}

	result, err := c.CallTool(ctx, callRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %q: %w", toolName, err)
	}

	return result, nil
}

// Close closes all clients
func (m *ClientManager) Close() {
	for _, c := range m.clients {
		c.Close()
	}
}

// ListServerNames returns a list of server names from the configuration
func (m *ClientManager) ListServerNames() []string {
	names := make([]string, 0, len(m.config.MCPServers))
	for name := range m.config.MCPServers {
		names = append(names, name)
	}
	return names
}
