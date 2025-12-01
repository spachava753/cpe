package codemode

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/cpe/internal/mcp"
)

// PartitionTools separates tools into code-mode and excluded categories.
// Tools in the excludedToolNames list will be returned as excludedTools for normal registration.
// All other tools will be grouped by server in serverToolsInfo for code mode access.
//
// Returns:
//   - serverToolsInfo: tools to be accessed via code mode, grouped by server
//   - excludedTools: tools to be registered normally (flat list)
func PartitionTools(
	toolsByServer map[string][]mcp.ToolData,
	mcpServers map[string]mcp.ServerConfig,
	excludedToolNames []string,
) ([]ServerToolsInfo, []mcp.ToolData) {
	// Build a set of excluded tool names for fast lookup
	excludedSet := make(map[string]bool, len(excludedToolNames))
	for _, name := range excludedToolNames {
		excludedSet[name] = true
	}

	var serverToolsInfo []ServerToolsInfo
	var excludedTools []mcp.ToolData

	for serverName, tools := range toolsByServer {
		serverConfig := mcpServers[serverName]

		var codeModeTools []*mcpsdk.Tool
		for _, toolData := range tools {
			if excludedSet[toolData.Tool.Name] {
				excludedTools = append(excludedTools, toolData)
			} else {
				codeModeTools = append(codeModeTools, toolData.MCPTool)
			}
		}

		// Only add server to code mode if it has tools for code mode
		if len(codeModeTools) > 0 {
			serverToolsInfo = append(serverToolsInfo, ServerToolsInfo{
				ServerName: serverName,
				Config:     serverConfig,
				Tools:      codeModeTools,
			})
		}
	}

	return serverToolsInfo, excludedTools
}

// GetCodeModeToolNames extracts tool names from ServerToolsInfo for collision detection.
func GetCodeModeToolNames(serverToolsInfo []ServerToolsInfo) []string {
	var names []string
	for _, server := range serverToolsInfo {
		for _, tool := range server.Tools {
			names = append(names, tool.Name)
		}
	}
	return names
}
