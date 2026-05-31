package textedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
)

func TestMakeToolReturnsTextEditTool(t *testing.T) {
	t.Parallel()

	tool, callback := MakeTool()
	if tool.Name != ToolName {
		t.Fatalf("tool name = %q, want %q", tool.Name, ToolName)
	}
	if tool.Description == "" {
		t.Fatal("expected tool description")
	}
	if tool.InputSchema == nil {
		t.Fatal("expected input schema")
	}
	if callback == nil {
		t.Fatal("expected callback")
	}
}

func TestMakeToolCallbackAppliesTextEdit(t *testing.T) {
	t.Parallel()

	_, callback := MakeTool()
	path := filepath.Join(t.TempDir(), "file.txt")

	msg, err := callback.Call(context.Background(), map[string]any{
		"path":     path,
		"new_text": "hello",
	})
	if err != nil {
		t.Fatalf("callback returned error: %v", err)
	}
	if msg.Role != gai.ToolResult || msg.ToolResultError {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if len(msg.Blocks) != 1 || !strings.Contains(msg.Blocks[0].Content.String(), "created ") {
		t.Fatalf("unexpected callback result: %#v", msg.Blocks)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want hello", string(got))
	}
}

func TestMakeToolCallbackReturnsToolResultError(t *testing.T) {
	t.Parallel()

	_, callback := MakeTool()
	msg, err := callback.Call(context.Background(), map[string]any{
		"path": " ",
	})
	if err != nil {
		t.Fatalf("callback returned fatal error: %v", err)
	}
	if !msg.ToolResultError {
		t.Fatalf("expected tool result error, got %#v", msg)
	}
	if len(msg.Blocks) != 1 || !strings.Contains(msg.Blocks[0].Content.String(), "path is required") {
		t.Fatalf("unexpected callback result: %#v", msg.Blocks)
	}
}
