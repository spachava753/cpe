package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/mcp"
)

// ClientManager manages connections to MCP servers
type ClientManager struct {
	clients map[string]*mcp.Client
	config  *ConfigFile
}

// NewClientManager creates a new ClientManager
func NewClientManager(config *ConfigFile) *ClientManager {
	// If the config is nil, create a default one with CPE's native tools
	if config == nil {
		config = &ConfigFile{MCPServers: make(map[string]MCPServerConfig)}
	}

	// If no servers are configured
	if len(config.MCPServers) == 0 {
		fmt.Fprintf(os.Stderr, "WARNING: No MCP servers configured.\n")
	}

	return &ClientManager{
		clients: make(map[string]*mcp.Client),
		config:  config,
	}
}

// GetClient returns an initialized client for the specified server
func (m *ClientManager) GetClient(ctx context.Context, serverName string) (*mcp.Client, error) {
	// Check if client already exists
	if c, ok := m.clients[serverName]; ok {
		return c, nil
	}

	// Get server config
	serverConfig, ok := m.config.MCPServers[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found in configuration", serverName)
	}

	// Create transport based on type
	var transport mcp.Transport
	var err error

	switch serverConfig.Type {
	case "", "stdio":
		// Convert env map to StdioConfig env map
		stdioConfig := mcp.StdioConfig{
			Command: serverConfig.Command,
			Args:    serverConfig.Args,
			Env:     serverConfig.Env,
			Timeout: serverConfig.Timeout,
		}
		transport = mcp.NewStdio(stdioConfig)
	case "sse":
		// SSE transport uses HTTPConfig
		httpConfig := mcp.HTTPConfig{
			URL:     serverConfig.URL,
			Timeout: serverConfig.Timeout,
		}
		// TODO: Add OAuth support when config structure supports it
		transport = mcp.NewHTTPSSE(httpConfig)
	case "http":
		// Streamable HTTP transport
		httpConfig := mcp.HTTPConfig{
			URL:     serverConfig.URL,
			Timeout: serverConfig.Timeout,
		}
		transport = mcp.NewStreamableHTTP(httpConfig)
	default:
		return nil, fmt.Errorf("unsupported client type: %s", serverConfig.Type)
	}

	// Create client info
	clientInfo := mcp.ClientInfo{
		Name:    "cpe",
		Version: "1.0.0", //TODO: get the bundled cpe version from debug info like we do with the version flag and use that here
	}

	// Create client capabilities
	capabilities := mcp.ClientCapabilities{
		// We don't need any specific capabilities for now
	}

	// Create and initialize the client
	c, err := mcp.NewClient(ctx, transport, clientInfo, capabilities, mcp.DefaultOptions())
	if err != nil {
		return nil, fmt.Errorf("failed to create and initialize client for server %s: %w", serverName, err)
	}

	// Store the client for future use
	m.clients[serverName] = c

	return c, nil
}

// ListTools lists the available tools from an MCP server
func (m *ClientManager) ListTools(ctx context.Context, serverName string) ([]gai.Tool, error) {
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

	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	return tools, nil
}

// CallTool calls a tool on an MCP server
func (m *ClientManager) CallTool(ctx context.Context, serverName string, toolName string, args map[string]interface{}) (gai.Message, error) {
	c, err := m.GetClient(ctx, serverName)
	if err != nil {
		return gai.Message{}, err
	}

	// Get timeout setting
	serverConfig := m.config.MCPServers[serverName]
	timeoutSeconds := 60 // Default timeout
	if serverConfig.Timeout > 0 {
		timeoutSeconds = serverConfig.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	result, err := c.CallTool(ctx, toolName, args)
	if err != nil {
		return gai.Message{}, fmt.Errorf("failed to call tool %q: %w", toolName, err)
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
