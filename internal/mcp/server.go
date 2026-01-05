package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/cpe/internal/version"
)

// SubagentDef defines a subagent for MCP server mode.
type SubagentDef struct {
	// Name is the tool name exposed via MCP (required)
	Name string
	// Description is the tool description exposed via MCP (required)
	Description string
	// OutputSchemaPath is an optional path to a JSON schema file for structured output
	OutputSchemaPath string
}

// MCPServerConfig holds the configuration needed to run an MCP server.
type MCPServerConfig struct {
	// Subagent is the subagent configuration (required)
	Subagent SubagentDef

	// MCPServers are the MCP server configurations available to the subagent
	MCPServers map[string]ServerConfig
}

// ServerOptions contains options for creating an MCP server
type ServerOptions struct {
	// Currently empty, reserved for future options like custom logging
}

// Server wraps an MCP server that exposes a subagent as a tool
type Server struct {
	config    MCPServerConfig
	opts      ServerOptions
	mcpServer *mcp.Server
}

// NewServer creates a new MCP server from the given configuration.
// The config must have a valid Subagent configuration.
func NewServer(cfg MCPServerConfig, opts ServerOptions) (*Server, error) {
	if cfg.Subagent.Name == "" {
		return nil, fmt.Errorf("subagent name is required")
	}
	if cfg.Subagent.Description == "" {
		return nil, fmt.Errorf("subagent description is required")
	}

	// Create the underlying MCP server
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "cpe",
			Title:   "CPE MCP Server",
			Version: version.Get(),
		},
		nil,
	)

	return &Server{
		config:    cfg,
		opts:      opts,
		mcpServer: mcpServer,
	}, nil
}

// Serve starts the MCP server and blocks until the context is cancelled
// or the connection is closed. The server communicates via stdio.
func (s *Server) Serve(ctx context.Context) error {
	// Run the server on stdio transport
	// The MCP SDK handles graceful shutdown when context is cancelled
	transport := &mcp.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}
