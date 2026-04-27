package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/textedit"
)

func TestInitializeConnectionsBuiltinTextEdit(t *testing.T) {
	t.Parallel()

	state, err := InitializeConnections(context.Background(), map[string]mcpconfig.ServerConfig{
		"editor": {Type: "builtin"},
	})
	if err != nil {
		t.Fatalf("InitializeConnections returned error: %v", err)
	}
	defer state.Close()

	conn := state.Connections["editor"]
	if conn == nil {
		t.Fatal("expected editor connection")
	}
	if len(conn.Tools) != 1 || conn.Tools[0].Name != textedit.ToolName {
		t.Fatalf("tools = %#v, want text_edit", conn.Tools)
	}

	path := filepath.Join(t.TempDir(), "file.txt")
	result, err := conn.ClientSession.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: textedit.ToolName,
		Arguments: map[string]any{
			"path": path,
			"text": "hello",
		},
	})
	if err != nil {
		t.Fatalf("CallTool returned protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool returned tool error: %#v", result.Content)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want hello", string(got))
	}
}

func TestInitializeConnectionsBuiltinToolFiltering(t *testing.T) {
	t.Parallel()

	state, err := InitializeConnections(context.Background(), map[string]mcpconfig.ServerConfig{
		"editor": {Type: "builtin", DisabledTools: []string{textedit.ToolName}},
	})
	if err != nil {
		t.Fatalf("InitializeConnections returned error: %v", err)
	}
	defer state.Close()

	if got := len(state.Connections["editor"].Tools); got != 0 {
		t.Fatalf("filtered tool count = %d, want 0", got)
	}
}

func TestInitializeConnectionsBuiltinDuplicateToolNameFails(t *testing.T) {
	t.Parallel()

	_, err := InitializeConnections(context.Background(), map[string]mcpconfig.ServerConfig{
		"editor-a": {Type: "builtin"},
		"editor-b": {Type: "builtin"},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate tool name \"text_edit\"") {
		t.Fatalf("expected duplicate tool error, got %v", err)
	}
}
