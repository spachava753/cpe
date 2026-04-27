package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/mcpconfig"
	"github.com/spachava753/cpe/internal/render"
)

func TestMCPListToolsSupportsBuiltinServer(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := MCPListTools(context.Background(), MCPListToolsOptions{
		MCPServers: map[string]mcpconfig.ServerConfig{
			"editor": {Type: "builtin"},
		},
		ServerName: "editor",
		Writer:     &out,
		Renderer:   &render.PlainTextRenderer{},
	})
	if err != nil {
		t.Fatalf("MCPListTools returned error: %v", err)
	}
	if !strings.Contains(out.String(), "text_edit") {
		t.Fatalf("expected text_edit in output, got:\n%s", out.String())
	}
}

func TestMCPCallToolSupportsBuiltinServer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "created.txt")
	var out bytes.Buffer
	err := MCPCallTool(context.Background(), MCPCallToolOptions{
		MCPServers: map[string]mcpconfig.ServerConfig{
			"editor": {Type: "builtin"},
		},
		ServerName: "editor",
		ToolName:   "text_edit",
		ToolArgs: map[string]any{
			"path": path,
			"text": "hello",
		},
		Writer: &out,
	})
	if err != nil {
		t.Fatalf("MCPCallTool returned error: %v", err)
	}
	if !strings.Contains(out.String(), "created ") {
		t.Fatalf("expected created message, got %q", out.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want hello", string(got))
	}
}
