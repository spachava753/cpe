package mcp

import (
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPConn holds runtime state for a single initialized MCP server.
// Tools are already filtered according to ServerConfig enabled/disabled rules.
type MCPConn struct {
	ServerName string
	Config     ServerConfig
	// ClientSession is the active MCP session. May be nil in test mocks
	// that don't invoke tool callbacks.
	ClientSession *mcpsdk.ClientSession
	Tools         []*mcpsdk.Tool // Already filtered per server config
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

// Close best-effort closes every non-nil client session and aggregates close errors.
func (s *MCPState) Close() error {
	var errs []error
	for name, conn := range s.Connections {
		if conn.ClientSession != nil {
			if err := conn.ClientSession.Close(); err != nil {
				errs = append(errs, fmt.Errorf("closing %s: %w", name, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing MCP connections: %v", errs)
	}
	return nil
}
