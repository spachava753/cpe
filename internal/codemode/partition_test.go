package codemode

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcp"
)

func TestPartitionTools(t *testing.T) {
	tests := []struct {
		name                    string
		toolsByServer           map[string][]mcp.ToolData
		mcpServers              map[string]mcp.ServerConfig
		excludedToolNames       []string
		wantCodeModeServerCount int
		wantCodeModeToolCount   int
		wantExcludedToolCount   int
	}{
		{
			name:                    "empty input",
			toolsByServer:           map[string][]mcp.ToolData{},
			mcpServers:              map[string]mcp.ServerConfig{},
			excludedToolNames:       nil,
			wantCodeModeServerCount: 0,
			wantCodeModeToolCount:   0,
			wantExcludedToolCount:   0,
		},
		{
			name: "all tools in code mode (no exclusions)",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
				},
			},
			mcpServers:              map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames:       nil,
			wantCodeModeServerCount: 1,
			wantCodeModeToolCount:   2,
			wantExcludedToolCount:   0,
		},
		{
			name: "all tools excluded",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
					{Tool: gai.Tool{Name: "tool2"}, MCPTool: &mcpsdk.Tool{Name: "tool2"}},
				},
			},
			mcpServers:              map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames:       []string{"tool1", "tool2"},
			wantCodeModeServerCount: 0,
			wantCodeModeToolCount:   0,
			wantExcludedToolCount:   2,
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
			mcpServers:              map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames:       []string{"tool2"},
			wantCodeModeServerCount: 1,
			wantCodeModeToolCount:   2,
			wantExcludedToolCount:   1,
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
			excludedToolNames:       []string{"tool2"},
			wantCodeModeServerCount: 2,
			wantCodeModeToolCount:   2,
			wantExcludedToolCount:   1,
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
			excludedToolNames:       []string{"tool2"},
			wantCodeModeServerCount: 1,
			wantCodeModeToolCount:   1,
			wantExcludedToolCount:   1,
		},
		{
			name: "exclusion list with non-existent tool name",
			toolsByServer: map[string][]mcp.ToolData{
				"server1": {
					{Tool: gai.Tool{Name: "tool1"}, MCPTool: &mcpsdk.Tool{Name: "tool1"}},
				},
			},
			mcpServers:              map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			excludedToolNames:       []string{"nonexistent"},
			wantCodeModeServerCount: 1,
			wantCodeModeToolCount:   1,
			wantExcludedToolCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverToolsInfo, excludedTools := PartitionTools(tt.toolsByServer, tt.mcpServers, tt.excludedToolNames)

			if len(serverToolsInfo) != tt.wantCodeModeServerCount {
				t.Errorf("got %d code mode servers, want %d", len(serverToolsInfo), tt.wantCodeModeServerCount)
			}

			var totalCodeModeTools int
			for _, server := range serverToolsInfo {
				totalCodeModeTools += len(server.Tools)
			}
			if totalCodeModeTools != tt.wantCodeModeToolCount {
				t.Errorf("got %d code mode tools, want %d", totalCodeModeTools, tt.wantCodeModeToolCount)
			}

			if len(excludedTools) != tt.wantExcludedToolCount {
				t.Errorf("got %d excluded tools, want %d", len(excludedTools), tt.wantExcludedToolCount)
			}
		})
	}
}

func TestGetCodeModeToolNames(t *testing.T) {
	tests := []struct {
		name            string
		serverToolsInfo []ServerToolsInfo
		wantNames       []string
	}{
		{
			name:            "empty",
			serverToolsInfo: nil,
			wantNames:       nil,
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
			wantNames: []string{"tool1", "tool2"},
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
			wantNames: []string{"tool1", "tool2", "tool3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := GetCodeModeToolNames(tt.serverToolsInfo)

			if len(names) != len(tt.wantNames) {
				t.Errorf("got %d names, want %d", len(names), len(tt.wantNames))
				return
			}

			// Create a set of expected names for checking
			wantSet := make(map[string]bool)
			for _, n := range tt.wantNames {
				wantSet[n] = true
			}

			for _, name := range names {
				if !wantSet[name] {
					t.Errorf("unexpected tool name %q", name)
				}
			}
		})
	}
}
