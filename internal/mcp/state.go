package mcp

import (
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

// MCPConn holds runtime state for a single initialized MCP server.
// Tools are already filtered according to ServerConfig enabled/disabled rules.
type MCPConn struct {
	ServerName string
	Config     mcpconfig.ServerConfig
	// ClientSession is the active MCP session. May be nil in test mocks
	// that don't invoke tool callbacks.
	ClientSession *mcpsdk.ClientSession
	Tools         []*mcpsdk.Tool // Already filtered per server config
	AllTools      []*mcpsdk.Tool // Unfiltered tools reported by the server
	FilteredOut   []string       // Tool names removed by per-server filtering

	close func() error
}

// MCPState tracks all active MCP connections keyed by configured server name.
type MCPState struct {
	Connections map[string]*MCPConn // Exported, keyed by server name
}

// NewMCPState creates an empty MCPState ready for incremental connection setup.
func NewMCPState() *MCPState {
	return &MCPState{
		Connections: make(map[string]*MCPConn),
	}
}

// Close best-effort closes the client session and any owned builtin server.
func (c *MCPConn) Close() error {
	var errs []error
	if c.ClientSession != nil {
		if err := c.ClientSession.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.close != nil {
		if err := c.close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing MCP connection %s: %v", c.ServerName, errs)
	}
	return nil
}

// Close best-effort closes every non-nil client session and aggregates close errors.
func (s *MCPState) Close() error {
	var errs []error
	for name, conn := range s.Connections {
		if err := conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing MCP connections: %v", errs)
	}
	return nil
}
