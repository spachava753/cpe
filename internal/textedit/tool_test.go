package textedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
)

const testSessionID acp.SessionId = "session-1"

type recordingSessionUpdator struct {
	updates []acp.SessionNotification
}

func (r *recordingSessionUpdator) SessionUpdate(ctx context.Context, params *acp.SessionNotification) error {
	r.updates = append(r.updates, *params)
	return nil
}

var _ sessionUpdator = (*recordingSessionUpdator)(nil)

func makeTestTool(t *testing.T) (gai.Tool, gai.ToolCallback, *recordingSessionUpdator) {
	t.Helper()

	updator := &recordingSessionUpdator{}
	tool, callback := MakeTool(testSessionID, updator)
	return tool, callback, updator
}

func requireToolCallUpdate(t *testing.T, got acp.SessionNotification, wantID acp.ToolCallId, wantStatus acp.ToolCallStatus) acp.SessionUpdate {
	t.Helper()

	if got.SessionID != testSessionID {
		t.Fatalf("SessionId = %q, want %q", got.SessionID, testSessionID)
	}
	update := got.Update
	if update.SessionUpdate != acp.SessionUpdateTypeToolCallUpdate {
		t.Fatalf("SessionUpdate = %q, want %q in %#v", update.SessionUpdate, acp.SessionUpdateTypeToolCallUpdate, update)
	}
	if update.ToolCallID != wantID {
		t.Fatalf("ToolCallId = %q, want %q", update.ToolCallID, wantID)
	}
	if update.Status == nil || *update.Status != wantStatus {
		t.Fatalf("Status = %#v, want %q", update.Status, wantStatus)
	}
	return update
}

func TestMakeToolReturnsTextEditTool(t *testing.T) {
	t.Parallel()

	tool, callback := MakeTool(testSessionID, nil)
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

// wantContent is the file body written and asserted across edit tests.
const wantContent = "hello"

func TestMakeToolCallbackAppliesTextEdit(t *testing.T) {
	t.Parallel()

	_, callback, updator := makeTestTool(t)
	path := filepath.Join(t.TempDir(), "file.txt")
	toolCallID := acp.ToolCallId("call-1")

	msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), map[string]any{
		"path":     path,
		"new_text": wantContent,
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
	if string(got) != wantContent {
		t.Fatalf("content = %q, want hello", string(got))
	}

	if len(updator.updates) != 2 {
		t.Fatalf("updates len = %d, want 2: %#v", len(updator.updates), updator.updates)
	}
	inProgress := requireToolCallUpdate(t, updator.updates[0], toolCallID, acp.ToolCallStatusInProgress)
	if inProgress.Kind == nil || *inProgress.Kind != acp.ToolKindEdit {
		t.Fatalf("Kind = %#v, want %q", inProgress.Kind, acp.ToolKindEdit)
	}

	completed := requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusCompleted)
	if completed.Kind == nil || *completed.Kind != acp.ToolKindEdit {
		t.Fatalf("Kind = %#v, want %q", completed.Kind, acp.ToolKindEdit)
	}
	content, ok := completed.Content.([]acp.ToolCallContent)
	if !ok || len(content) != 1 || content[0].Type != acp.ToolCallContentTypeDiff {
		t.Fatalf("completed content = %#v, want one diff", completed.Content)
	}
	diff := content[0]
	if diff.Path != path || diff.NewText != wantContent {
		t.Fatalf("diff = %#v, want path %q and new text hello", diff, path)
	}
}

func TestMakeToolCallbackReturnsToolResultError(t *testing.T) {
	t.Parallel()

	_, callback, updator := makeTestTool(t)
	toolCallID := acp.ToolCallId("call-2")

	msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), map[string]any{
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

	if len(updator.updates) != 2 {
		t.Fatalf("updates len = %d, want 2: %#v", len(updator.updates), updator.updates)
	}
	inProgress := requireToolCallUpdate(t, updator.updates[0], toolCallID, acp.ToolCallStatusInProgress)
	if inProgress.Kind == nil || *inProgress.Kind != acp.ToolKindEdit {
		t.Fatalf("Kind = %#v, want %q", inProgress.Kind, acp.ToolKindEdit)
	}
	requireToolCallUpdate(t, updator.updates[1], toolCallID, acp.ToolCallStatusFailed)
}
