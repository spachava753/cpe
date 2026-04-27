package codemode

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcp"
)

// PartitionTools splits MCP tools into code-mode exposure and regular callback exposure.
// Builtin server tools are always excluded from code mode. Name matching is exact
// against excludedToolNames across all non-builtin servers.
//
// Returns:
//   - codeModeServers: per-server tool lists used for generated code bindings
//   - excludedByServer: tools removed from code mode so callers can register normal callbacks
//
// Servers with zero remaining code-mode tools are omitted from codeModeServers.
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
		if mcp.EffectiveServerType(conn.Config) == "builtin" {
			excludedByServer[serverName] = append(excludedByServer[serverName], conn.Tools...)
			continue
		}

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

// GetCodeModeToolNames flattens code-mode tool names for collision checks.
// Duplicate names are intentionally preserved so validators can detect conflicts.
func GetCodeModeToolNames(servers []*mcp.MCPConn) []string {
	var names []string
	for _, server := range servers {
		for _, tool := range server.Tools {
			names = append(names, tool.Name)
		}
	}
	return names
}
