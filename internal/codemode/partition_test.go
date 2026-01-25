package codemode

import (
	"sort"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcp"
)

func TestPartitionTools(t *testing.T) {
	tests := []struct {
		name              string
		toolsByServer     map[string][]mcp.ToolData
		mcpServers        map[string]mcp.ServerConfig
		excludedToolNames []string
	}{
		{
			name:              "empty input",
			toolsByServer:     map[string][]mcp.ToolData{},
			mcpServers:        map[string]mcp.ServerConfig{},
			excludedToolNames: nil,
		},
		{
			name: "all tools in code mode (no exclusions)",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
				},
			},
			mcpServers:        map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames: nil,
		},
		{
			name: "all tools excluded",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
				},
			},
			mcpServers:        map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames: []string{"tool1", "tool2"},
		},
		{
			name: "mixed: some excluded, some in code mode",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
					{Tool: gai.Tool{Name: "tool3"}, MCPTool: &mcpsdk.Tool{Name: "tool3"}},
				},
			},
			mcpServers:        map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames: []string{"tool2"},
		},
		{
			name: "multiple servers",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
				},
				"server2": {
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
					{Tool: gai.Tool{Name: "tool3"}, MCPTool: &mcpsdk.Tool{Name: "tool3"}},
				},
			},
			mcpServers: map[string]mcp.ServerConfig{
				"server1": {Type: "stdio", Command: "test1"},
				"server2": {Type: "http", URL: "http://test"},
			},
			excludedToolNames: []string{"tool2"},
		},
		{
			name: "server with all tools excluded is not included",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
				},
				"server2": {
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
				},
			},
			mcpServers: map[string]mcp.ServerConfig{
				"server1": {Type: "stdio", Command: "test1"},
				"server2": {Type: "stdio", Command: "test2"},
			},
			excludedToolNames: []string{"tool2"},
		},
		{
			name: "exclusion list with non-existent tool name",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
				},
			},
			mcpServers:        map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames: []string{"nonexistent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverToolsInfo, excludedTools := PartitionTools(tt.toolsByServer, tt.mcpServers, tt.excludedToolNames)

			// Sort for deterministic snapshots
			sort.Slice(serverToolsInfo, func(i, j int) bool {
				return serverToolsInfo[i].ServerName < serverToolsInfo[j].ServerName
			})
			for i := range serverToolsInfo {
				sort.Slice(serverToolsInfo[i].Tools, func(a, b int) bool {
					return serverToolsInfo[i].Tools[a].Name < serverToolsInfo[i].Tools[b].Name
				})
			}
			sort.Slice(excludedTools, func(i, j int) bool {
				return excludedTools[i].Tool.Name < excludedTools[j].Tool.Name
			})

			cupaloy.SnapshotT(t, serverToolsInfo, excludedTools)
		})
	}
}

func TestGetCodeModeToolNames(t *testing.T) {
	tests := []struct {
		name            string
		serverToolsInfo []ServerToolsInfo
	}{
		{
			name:            "empty",
			serverToolsInfo: nil,
		},
		{
			name: "single server with tools",
			serverToolsInfo: []ServerToolsInfo{
				{
					ServerName: "server1",
					Tools: []*mcpsdk.Tool{
						{Name: "tool1"},
						{Name: "tool2"},
					},
				},
			},
		},
		{
			name: "multiple servers",
			serverToolsInfo: []ServerToolsInfo{
				{
					ServerName: "server1",
					Tools:      []*mcpsdk.Tool{{Name: "tool1"}},
				},
				{
					ServerName: "server2",
					Tools:      []*mcpsdk.Tool{{Name: "tool2"}, {Name: "tool3"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := GetCodeModeToolNames(tt.serverToolsInfo)

			// Sort for deterministic snapshots
			sort.Strings(names)

			cupaloy.SnapshotT(t, names)
		})
	}
}
