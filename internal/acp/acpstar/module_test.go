package acpstar

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	acpsdk "github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spachava753/starlarkx/starlark"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/storage"
)

func newACPSessionTestStore(t *testing.T) *storage.Sqlite {
	t.Helper()

	store, err := storage.NewConvoDB(t.Context(), filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewConvoDB() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func saveACPTestDialog(t *testing.T, store *storage.Sqlite, dialog gai.Dialog) gai.Dialog {
	t.Helper()

	saved := make(gai.Dialog, 0, len(dialog))
	for message, err := range store.SaveDialog(t.Context(), slices.Values(dialog)) {
		if err != nil {
			t.Fatalf("SaveDialog() error = %v", err)
		}
		saved = append(saved, message)
	}
	if len(saved) != len(dialog) {
		t.Fatalf("SaveDialog() returned %d messages, want %d", len(saved), len(dialog))
	}
	return saved
}

func newACPTestSphere(t *testing.T, store SessionStore, sessionID acpsdk.SessionId, cwd string) (*dyson.Sphere, *bytes.Buffer) {
	t.Helper()

	output := new(bytes.Buffer)
	sphere := dyson.NewSphere(
		func(_ *starlark.Thread, message string) { fmt.Fprint(output, message) },
		Module(store, sessionID, cwd),
	)
	t.Cleanup(func() {
		if err := sphere.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return sphere, output
}

func evalACPTestSphere(ctx context.Context, sphere *dyson.Sphere, output *bytes.Buffer, code string) (string, error) {
	output.Reset()
	err := sphere.Eval(ctx, code)
	return output.String(), err
}

func TestModuleReadsTypedCurrentAndPersistedSessions(t *testing.T) {
	t.Parallel()

	store := newACPSessionTestStore(t)
	activeCall := gai.Block{
		ID:           "call-current",
		BlockType:    gai.ToolCall,
		ModalityType: gai.Text,
		Content:      gai.Str(`{"name":"starlark_repl","parameters":{"count":3,"large":123456789012345678901234567890,"nested":{"ok":true,"values":["x",null,2.5]}}}`),
	}
	saved := saveACPTestDialog(t, store, gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{
			gai.TextBlock("old question"),
			{
				BlockType:    gai.Content,
				ModalityType: gai.Image,
				MimeType:     "image/png",
				Content:      gai.Str("aW1hZ2U="),
				ExtraFields:  map[string]any{gai.BlockFieldFilenameKey: "evidence.png"},
			},
		}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("old answer")}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("new question")}},
		{Role: gai.Assistant, Blocks: []gai.Block{
			{BlockType: gai.Thinking, ModalityType: gai.Text, Content: gai.Str("working")},
			activeCall,
		}},
	})
	priorHeadID := storage.GetMessageID(saved[1])
	activeMessageID := storage.GetMessageID(saved[3])

	for _, params := range []storage.CreateACPSessionParams{
		{
			Session: acpsdk.SessionInfo{
				SessionID: "previous",
				Cwd:       "/workspace/current",
				Title:     new("Previous session"),
			},
			LastMessageID: priorHeadID,
		},
		{
			Session: acpsdk.SessionInfo{
				SessionID: "other-project",
				Cwd:       "/workspace/other",
				Title:     new("Other project"),
			},
			LastMessageID: priorHeadID,
		},
		{
			Session: acpsdk.SessionInfo{
				SessionID: "current",
				Cwd:       "/workspace/current",
				Title:     new("Current session"),
			},
			// The committed head intentionally trails the active tool-call message.
			LastMessageID: priorHeadID,
		},
	} {
		if err := store.CreateACPSession(t.Context(), params); err != nil {
			t.Fatalf("CreateACPSession(%q) error = %v", params.Session.SessionID, err)
		}
	}

	sphere, output := newACPTestSphere(t, store, "current", "/workspace/current")
	ctx := xctx.WithExecutionMessageID(t.Context(), activeMessageID)
	got, err := evalACPTestSphere(ctx, sphere, output, `load("acp.star", "acp")
current = acp.get_session()
explicit_current = acp.get_session(id="current")
previous = acp.get_session("previous")
call = current.messages[-1].blocks[1]
attachment = current.messages[0].blocks[1]
print(type(current))
print(type(current.messages[-1]))
print(type(call))
print(current)
print(current.messages[-1])
print(call)
print(dir(acp))
print(dir(current))
print(type(current.messages), type(current.messages[-1].blocks))
print(current.id, current.cwd, current.title)
print(current.last_message_id, len(current.messages))
print(current.messages[-1].role, current.messages[-1].blocks[0].modality)
print(call.modality, call.mime_type, call.filename)
print(attachment.kind, attachment.modality, attachment.mime_type, attachment.filename, attachment.content)
print(call.content)
print(call.arguments)
print(type(call.arguments["count"]), type(call.arguments["large"]), type(call.arguments["nested"]["values"][2]))
print(explicit_current.last_message_id == current.last_message_id)
print(previous.last_message_id, len(previous.messages))
print(current.messages[-1].parent_id == current.messages[-2].id)
print(acp.list_sessions())
print(acp.list_sessions(cwd=None))
print(acp.list_sessions(cwd="/workspace/other"))`)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	want := fmt.Sprintf(`acp.Session
acp.Message
acp.Block
acp.Session(id="current", messages=4)
acp.Message(id=%q, role="assistant", blocks=2)
acp.Block(kind="tool_call", id="call-current", name="starlark_repl")
["get_session", "list_sessions"]
["cwd", "id", "last_message_id", "messages", "title"]
tuple tuple
current /workspace/current Current session
%s 4
assistant text
None None None
content image image/png evidence.png aW1hZ2U=
{"name":"starlark_repl","parameters":{"count":3,"large":123456789012345678901234567890,"nested":{"ok":true,"values":["x",null,2.5]}}}
{"count": 3, "large": 123456789012345678901234567890, "nested": {"ok": True, "values": ["x", None, 2.5]}}
int int float
True
%s 2
True
["current", "previous"]
["current", "previous"]
["other-project"]
`, activeMessageID, activeMessageID, priorHeadID)
	if got != want {
		t.Fatalf("Eval() output:\n%s\nwant:\n%s", got, want)
	}

	nextCall := gai.Block{
		ID:           "call-next",
		BlockType:    gai.ToolCall,
		ModalityType: gai.Text,
		Content:      gai.Str(`{"name":"starlark_repl","parameters":{}}`),
	}
	extended := append(slices.Clone(saved),
		gai.Message{
			Role:            gai.ToolResult,
			ToolResultError: true,
			Blocks: []gai.Block{{
				ID:           "call-current",
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("failed result"),
			}},
		},
		gai.Message{Role: gai.Assistant, Blocks: []gai.Block{nextCall}},
	)
	extended = saveACPTestDialog(t, store, extended)
	nextMessageID := storage.GetMessageID(extended[len(extended)-1])

	// The acp binding came from the first evaluation, but must use this call's
	// new execution-scoped cutoff and context.
	ctx = xctx.WithExecutionMessageID(t.Context(), nextMessageID)
	got, err = evalACPTestSphere(ctx, sphere, output, `latest = acp.get_session()
print(latest.last_message_id, len(latest.messages))
print(latest.messages[-2].role, latest.messages[-2].tool_result_error, latest.messages[-2].blocks[0].content)`)
	if err != nil {
		t.Fatalf("second Eval() error = %v", err)
	}
	want = fmt.Sprintf("%s 6\ntool_result True failed result\n", nextMessageID)
	if got != want {
		t.Fatalf("second Eval() output = %q, want %q", got, want)
	}

	_, err = evalACPTestSphere(ctx, sphere, output, `call.arguments["count"] = 4`)
	if err == nil || !strings.Contains(err.Error(), "frozen hash table") {
		t.Fatalf("frozen arguments error = %v, want immutable dictionary error", err)
	}

	_, err = evalACPTestSphere(ctx, sphere, output, `call.arguments["nested"]["values"].append("y")`)
	if err == nil || !strings.Contains(err.Error(), "frozen list") {
		t.Fatalf("frozen nested list error = %v, want immutable list error", err)
	}

	_, err = evalACPTestSphere(ctx, sphere, output, `current.id = "changed"`)
	if err == nil || !strings.Contains(err.Error(), "can't assign to .id field of acp.Session") {
		t.Fatalf("read-only attribute error = %v, want immutable attribute error", err)
	}

	_, err = evalACPTestSphere(ctx, sphere, output, `print({current: True})`)
	if err == nil || !strings.Contains(err.Error(), "unhashable: acp.Session") {
		t.Fatalf("hash error = %v, want unhashable acp.Session error", err)
	}
}

func TestModuleReturnsCompleteCompactionHistory(t *testing.T) {
	t.Parallel()

	store := newACPSessionTestStore(t)
	before := saveACPTestDialog(t, store, gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("before compaction")}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("old answer")}},
	})
	beforeLeafID := storage.GetMessageID(before[len(before)-1])
	compacted := saveACPTestDialog(t, store, gai.Dialog{
		{
			Role:   gai.User,
			Blocks: []gai.Block{gai.TextBlock("compacted summary")},
			ExtraFields: map[string]any{
				storage.MessageCompactionParentIDKey: beforeLeafID,
			},
		},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("after compaction")}},
	})
	lastMessageID := storage.GetMessageID(compacted[len(compacted)-1])
	if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acpsdk.SessionInfo{
			SessionID: "compacted",
			Cwd:       "/workspace/compacted",
			Title:     new("Compacted session"),
		},
		LastMessageID: lastMessageID,
	}); err != nil {
		t.Fatalf("CreateACPSession() error = %v", err)
	}

	sphere, output := newACPTestSphere(t, store, "compacted", "/workspace/compacted")
	ctx := xctx.WithExecutionMessageID(t.Context(), lastMessageID)
	got, err := evalACPTestSphere(ctx, sphere, output, `load("acp.star", "acp")
session = acp.get_session()
print(len(session.messages))
for message in session.messages:
    print(message.role, message.blocks[0].content, message.compaction_parent_id)`)
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	want := fmt.Sprintf("4\nuser before compaction None\nassistant old answer None\nuser compacted summary %s\nassistant after compaction None\n", beforeLeafID)
	if got != want {
		t.Fatalf("Eval() output = %q, want %q", got, want)
	}
}

func TestModuleReturnsEmptyPersistedSession(t *testing.T) {
	t.Parallel()

	store := newACPSessionTestStore(t)
	if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acpsdk.SessionInfo{
			SessionID: "empty",
			Cwd:       "/workspace/empty",
			Title:     new("Empty session"),
		},
	}); err != nil {
		t.Fatalf("CreateACPSession() error = %v", err)
	}
	sphere, output := newACPTestSphere(t, store, "runner", "/workspace/runner")
	got, err := evalACPTestSphere(t.Context(), sphere, output, "load(\"acp.star\", \"acp\")\nsession = acp.get_session(\"empty\")\nprint(session.last_message_id, session.messages, len(session.messages))")
	if err != nil {
		t.Fatalf("Eval() error = %v", err)
	}
	if got != "None () 0\n" {
		t.Fatalf("Eval() output = %q, want empty typed session", got)
	}
}

func TestModuleErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid arguments", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			code      string
			parameter string
		}{
			{
				name:      "session ID",
				code:      "load(\"acp.star\", \"acp\")\nacp.get_session(id=1)",
				parameter: "id",
			},
			{
				name:      "working directory",
				code:      "load(\"acp.star\", \"acp\")\nacp.list_sessions(cwd=1)",
				parameter: "cwd",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				sphere, output := newACPTestSphere(t, nil, "current", "/workspace")
				_, err := evalACPTestSphere(t.Context(), sphere, output, test.code)
				if err == nil || !strings.Contains(err.Error(), `for parameter "`+test.parameter+`"`) {
					t.Fatalf("Eval() error = %v, want invalid %s error", err, test.parameter)
				}
			})
		}
	})

	t.Run("store unavailable", func(t *testing.T) {
		sphere, output := newACPTestSphere(t, nil, "current", "/workspace")
		_, err := evalACPTestSphere(t.Context(), sphere, output, "load(\"acp.star\", \"acp\")\nacp.list_sessions()")
		if err == nil || !strings.Contains(err.Error(), "persisted session store is unavailable") {
			t.Fatalf("Eval() error = %v, want unavailable store error", err)
		}
	})

	t.Run("current cutoff missing", func(t *testing.T) {
		store := newACPSessionTestStore(t)
		if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acpsdk.SessionInfo{SessionID: "current", Cwd: "/workspace", Title: new("Current")},
		}); err != nil {
			t.Fatalf("CreateACPSession() error = %v", err)
		}
		sphere, output := newACPTestSphere(t, store, "current", "/workspace")
		_, err := evalACPTestSphere(t.Context(), sphere, output, "load(\"acp.star\", \"acp\")\nacp.get_session()")
		if err == nil || !strings.Contains(err.Error(), "no persisted assistant message ID") {
			t.Fatalf("Eval() error = %v, want missing cutoff error", err)
		}
	})

	t.Run("missing session", func(t *testing.T) {
		store := newACPSessionTestStore(t)
		sphere, output := newACPTestSphere(t, store, "current", "/workspace")
		_, err := evalACPTestSphere(t.Context(), sphere, output, "load(\"acp.star\", \"acp\")\nacp.get_session(\"missing\")")
		if err == nil || !strings.Contains(err.Error(), `session "missing"`) {
			t.Fatalf("Eval() error = %v, want missing session error", err)
		}
	})

	t.Run("corrupt tool call", func(t *testing.T) {
		store := newACPSessionTestStore(t)
		saved := saveACPTestDialog(t, store, gai.Dialog{{
			Role: gai.Assistant,
			Blocks: []gai.Block{{
				ID:           "bad-call",
				BlockType:    gai.ToolCall,
				ModalityType: gai.Text,
				Content:      gai.Str(`{"name":`),
			}},
		}})
		lastMessageID := storage.GetMessageID(saved[0])
		if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session:       acpsdk.SessionInfo{SessionID: "corrupt", Cwd: "/workspace", Title: new("Corrupt")},
			LastMessageID: lastMessageID,
		}); err != nil {
			t.Fatalf("CreateACPSession() error = %v", err)
		}
		sphere, output := newACPTestSphere(t, store, "runner", "/workspace")
		_, err := evalACPTestSphere(t.Context(), sphere, output, "load(\"acp.star\", \"acp\")\nacp.get_session(\"corrupt\")")
		if err == nil || !strings.Contains(err.Error(), `decode tool call "bad-call"`) {
			t.Fatalf("Eval() error = %v, want corrupt tool call error", err)
		}
	})
}
