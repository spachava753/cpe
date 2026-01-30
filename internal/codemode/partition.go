package codemode

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcp"
)

// PartitionTools separates tools into code-mode and excluded categories.
// Tools in the excludedToolNames list will be returned as excludedByServer for normal registration.
// All other tools will be grouped by server in codeModeServers for code mode access.
//
// Returns:
//   - codeModeServers: MCPConn entries for code mode (used by maingen.go, ignores ClientSession)
//   - excludedByServer: Excluded tools grouped by server name (caller creates callbacks)
//
// Note: Servers with zero code-mode tools after partitioning are excluded from codeModeServers.
func PartitionTools(
	mcpState *mcp.MCPState,
	excludedToolNames []string,
) (codeModeServers []*mcp.MCPConn, excludedByServer map[string][]*mcpsdk.Tool) {
	// Build a set of excluded tool names for fast lookup
	excludedSet := make(map[string]bool, len(excludedToolNames))
	for _, name := range excludedToolNames {
		excludedSet[name] = true
	}

	excludedByServer = make(map[string][]*mcpsdk.Tool)

	for serverName, conn := range mcpState.Connections {
		var codeModeTools []*mcpsdk.Tool
		for _, tool := range conn.Tools {
			if excludedSet[tool.Name] {
				excludedByServer[serverName] = append(excludedByServer[serverName], tool)
			} else {
				codeModeTools = append(codeModeTools, tool)
			}
		}

		// Only add server to code mode if it has tools for code mode
		if len(codeModeTools) > 0 {
			codeModeServers = append(codeModeServers, &mcp.MCPConn{
				ServerName:    serverName,
				Config:        conn.Config,
				ClientSession: conn.ClientSession,
				Tools:         codeModeTools,
			})
		}
	}

	return codeModeServers, excludedByServer
}

// GetCodeModeToolNames extracts tool names from MCPConn entries for collision detection.
func GetCodeModeToolNames(servers []*mcp.MCPConn) []string {
	var names []string
	for _, server := range servers {
		for _, tool := range server.Tools {
			names = append(names, tool.Name)
		}
	}
	return names
}
