package codemode

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/mcpconfig"
)

func TestPartitionToolsExcludesBuiltinToolsFromCodeMode(t *testing.T) {
	t.Parallel()

	state := &mcp.MCPState{Connections: map[string]*mcp.MCPConn{
		"editor": {
			ServerName: "editor",
			Config:     mcpconfig.ServerConfig{Type: "builtin"},
			Tools:      []*mcpsdk.Tool{{Name: "text_edit"}},
		},
		"search": {
			ServerName: "search",
			Config:     mcpconfig.ServerConfig{Type: "stdio"},
			Tools:      []*mcpsdk.Tool{{Name: "web_search"}},
		},
	}}

	codeModeServers, excludedByServer := PartitionTools(state, nil)

	if len(codeModeServers) != 1 || codeModeServers[0].ServerName != "search" {
		t.Fatalf("codeModeServers = %#v, want only search", codeModeServers)
	}
	builtinExcluded := excludedByServer["editor"]
	if len(builtinExcluded) != 1 || builtinExcluded[0].Name != "text_edit" {
		t.Fatalf("builtin excluded tools = %#v, want text_edit", builtinExcluded)
	}
}
