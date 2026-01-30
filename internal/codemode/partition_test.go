package codemode

import (
	"sort"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcp"
)

// newTestMCPState creates an MCPState for testing
func newTestMCPState(connections map[string][]*mcpsdk.Tool, configs map[string]mcp.ServerConfig) *mcp.MCPState {
	state := mcp.NewMCPState()
	for serverName, tools := range connections {
		config := mcp.ServerConfig{Type: "stdio", Command: "test"}
		if c, ok := configs[serverName]; ok {
			config = c
		}
		state.Connections[serverName] = &mcp.MCPConn{
			ServerName:    serverName,
			Config:        config,
			ClientSession: nil,
			Tools:         tools,
		}
	}
	return state
}

func TestPartitionTools(t *testing.T) {
	tests := []struct {
		name              string
		mcpState          *mcp.MCPState
		excludedToolNames []string
	}{
		{
			name:              "empty input",
			mcpState:          mcp.NewMCPState(),
			excludedToolNames: nil,
		},
		{
			name: "all tools in code mode (no exclusions)",
			mcpState: newTestMCPState(
				map[string][]*mcpsdk.Tool{
					"server1": {
						{Name: "tool1"},
						{Name: "tool2"},
					},
				},
				map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			),
			excludedToolNames: nil,
		},
		{
			name: "all tools excluded",
			mcpState: newTestMCPState(
				map[string][]*mcpsdk.Tool{
					"server1": {
						{Name: "tool1"},
						{Name: "tool2"},
					},
				},
				map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			),
			excludedToolNames: []string{"tool1", "tool2"},
		},
		{
			name: "mixed: some excluded, some in code mode",
			mcpState: newTestMCPState(
				map[string][]*mcpsdk.Tool{
					"server1": {
						{Name: "tool1"},
						{Name: "tool2"},
						{Name: "tool3"},
					},
				},
				map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			),
			excludedToolNames: []string{"tool2"},
		},
		{
			name: "multiple servers",
			mcpState: newTestMCPState(
				map[string][]*mcpsdk.Tool{
					"server1": {
						{Name: "tool1"},
					},
					"server2": {
						{Name: "tool2"},
						{Name: "tool3"},
					},
				},
				map[string]mcp.ServerConfig{
					"server1": {Type: "stdio", Command: "test1"},
					"server2": {Type: "http", URL: "http://test"},
				},
			),
			excludedToolNames: []string{"tool2"},
		},
		{
			name: "server with all tools excluded is not included",
			mcpState: newTestMCPState(
				map[string][]*mcpsdk.Tool{
					"server1": {
						{Name: "tool1"},
					},
					"server2": {
						{Name: "tool2"},
					},
				},
				map[string]mcp.ServerConfig{
					"server1": {Type: "stdio", Command: "test1"},
					"server2": {Type: "stdio", Command: "test2"},
				},
			),
			excludedToolNames: []string{"tool2"},
		},
		{
			name: "exclusion list with non-existent tool name",
			mcpState: newTestMCPState(
				map[string][]*mcpsdk.Tool{
					"server1": {
						{Name: "tool1"},
					},
				},
				map[string]mcp.ServerConfig{"server1": {Type: "stdio", Command: "test"}},
			),
			excludedToolNames: []string{"nonexistent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codeModeServers, excludedByServer := PartitionTools(tt.mcpState, tt.excludedToolNames)

			// Sort for deterministic snapshots
			sort.Slice(codeModeServers, func(i, j int) bool {
				return codeModeServers[i].ServerName < codeModeServers[j].ServerName
			})
			for i := range codeModeServers {
				sort.Slice(codeModeServers[i].Tools, func(a, b int) bool {
					return codeModeServers[i].Tools[a].Name < codeModeServers[i].Tools[b].Name
				})
			}

			// Convert excludedByServer to sorted flat list for snapshot
			var excludedToolNames []string
			for serverName, tools := range excludedByServer {
				for _, tool := range tools {
					excludedToolNames = append(excludedToolNames, serverName+"/"+tool.Name)
				}
			}
			sort.Strings(excludedToolNames)

			cupaloy.SnapshotT(t, codeModeServers, excludedToolNames)
		})
	}
}

func TestGetCodeModeToolNames(t *testing.T) {
	tests := []struct {
		name    string
		servers []*mcp.MCPConn
	}{
		{
			name:    "empty",
			servers: nil,
		},
		{
			name: "single server with tools",
			servers: []*mcp.MCPConn{
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
			servers: []*mcp.MCPConn{
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
			names := GetCodeModeToolNames(tt.servers)

			// Sort for deterministic snapshots
			sort.Strings(names)

			cupaloy.SnapshotT(t, names)
		})
	}
}
