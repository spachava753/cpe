package acp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
)

const testSessionID acp.SessionId = "session-1"

type recordingACPConn struct {
	updates []acp.SessionNotification
	err     error
	errAt   int
}

func (r *recordingACPConn) SessionUpdate(_ context.Context, params *acp.SessionNotification) error {
	r.updates = append(r.updates, *params)
	if r.err != nil && (r.errAt == 0 || len(r.updates) == r.errAt) {
		return r.err
	}
	return nil
}

var _ acpConn = (*recordingACPConn)(nil)

func requireExecutionStatus(
	t *testing.T,
	got acp.SessionNotification,
	wantToolCallID acp.ToolCallId,
	wantStatus acp.ToolCallStatus,
) {
	t.Helper()

	if got.SessionID != testSessionID {
		t.Fatalf("SessionID = %q, want %q", got.SessionID, testSessionID)
	}
	if got.Update.ToolCallID != wantToolCallID {
		t.Fatalf("ToolCallID = %q, want %q", got.Update.ToolCallID, wantToolCallID)
	}
	if got.Update.Status == nil || *got.Update.Status != wantStatus {
		t.Fatalf("Status = %#v, want %q", got.Update.Status, wantStatus)
	}
	if got.Update.Kind == nil || *got.Update.Kind != acp.ToolKindOther {
		t.Fatalf("Kind = %#v, want %q", got.Update.Kind, acp.ToolKindOther)
	}
}

func TestStarlarkREPLCallbackSupportsOpenAndBytesDecode(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "note.txt"), []byte("hello from Dyson"), 0o600); err != nil {
		t.Fatal(err)
	}
	callback := &starlarkREPLCallback{MaxTimeout: 5, Cwd: cwd}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code": `file = open("note.txt")
print(file.read())
file.close()
load("os.star", "os")
fd = os.open("note.txt", os.O_RDONLY)
data = os.read(fd, 100)
os.close(fd)
print(data.decode("utf-8"))`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "hello from Dyson\nhello from Dyson\n" {
		t.Fatalf("Call() = %#v, want open and bytes.decode output", msg)
	}
}

func TestStarlarkREPLCallbackAllowsHTTPRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	callback := &starlarkREPLCallback{MaxTimeout: 5, Cwd: t.TempDir()}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code": fmt.Sprintf(`load("requests.star", "requests")
response = requests.get(%q)
print(response.status_code)
print(response.text)`, server.URL+"/status"),
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "200\n{\"ok\":true}\n" {
		t.Fatalf("Call() = %#v, want successful HTTP response", msg)
	}
}

func TestStarlarkREPLCallbackResetClosesOpenFiles(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "note.txt"), []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	callback := &starlarkREPLCallback{MaxTimeout: 5, Cwd: cwd}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             `file = open("note.txt")`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if msg.ToolResultError {
		t.Fatalf("Call() returned tool error: %#v", msg)
	}
	file := callback.repl.sphere

	callback.Reset()
	if callback.repl != nil {
		t.Fatal("Reset() retained the evaluator")
	}
	if err := file.Eval(t.Context(), `file.read()`); err == nil || !strings.Contains(err.Error(), "sphere is closed") {
		t.Fatalf("Eval() after Reset error = %v, want closed sphere", err)
	}
}

func TestStarlarkREPLCallbackPersistsStateAndResetStartsFreshSphere(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             "answer = 41\nprint(answer)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("first Call() error = %v", err)
	}
	if msg.ToolResultError {
		t.Fatalf("first Call() returned tool error: %#v", msg)
	}
	if len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "41\n" {
		t.Fatalf("first Call() blocks = %#v, want printed assignment", msg.Blocks)
	}
	firstSphere := callback.repl.sphere

	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             "print(answer + 1)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("second Call() error = %v", err)
	}
	if len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "42\n" {
		t.Fatalf("second Call() blocks = %#v, want persisted global", msg.Blocks)
	}

	callback.Reset()
	if callback.repl != nil {
		t.Fatal("Reset() retained the evaluator")
	}
	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             "print(answer)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() after Reset error = %v", err)
	}
	if !msg.ToolResultError || !strings.Contains(msg.Blocks[0].Content.String(), "undefined: answer") {
		t.Fatalf("Call() after Reset = %#v, want undefined prior global", msg)
	}
	if callback.repl.sphere == firstSphere {
		t.Fatal("Reset() reused the prior Dyson Sphere")
	}
}

func TestStarlarkREPLCallbackRepeatedSuccessfulCallsRemainUncancelled(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	for i := range 100 {
		msg, err := callback.Call(t.Context(), map[string]any{
			"code":             fmt.Sprintf("value = %d\nprint(value)", i),
			"executionTimeout": 2,
		})
		if err != nil {
			t.Fatalf("Call(%d) error = %v", i, err)
		}
		want := fmt.Sprintf("%d\n", i)
		if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != want {
			t.Fatalf("Call(%d) = %#v, want %q", i, msg, want)
		}
	}
}

func TestStarlarkREPLCallbackRunsHostSubprocessInSessionDirectory(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	callback := &starlarkREPLCallback{MaxTimeout: 5, Cwd: cwd}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code": `load("subprocess.star", "subprocess")
result = subprocess.run(["pwd"], capture_output=True, text=True)
print(result.stdout.strip())`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != cwd+"\n" {
		t.Fatalf("Call() = %#v, want host subprocess cwd %q", msg, cwd)
	}
}

func TestStarlarkREPLCallbackCancelsHostSubprocessAtExecutionTimeout(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5, Cwd: t.TempDir()}
	started := time.Now()
	msg, err := callback.Call(t.Context(), map[string]any{
		"code": `load("subprocess.star", "subprocess")
subprocess.run(["sleep", "10"])`,
		"executionTimeout": 1,
	})
	if err != nil {
		t.Fatalf("Call() error = %v, want recoverable tool result", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Call() took %v, want subprocess cancellation near timeout", elapsed)
	}
	if !msg.ToolResultError || len(msg.Blocks) != 1 || !strings.Contains(msg.Blocks[0].Content.String(), "execution timed out after 1s") {
		t.Fatalf("Call() = %#v, want timeout tool result", msg)
	}
}

func TestStarlarkREPLCallbackReturnsOnlyPrintedText(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             "1 + 2",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("expression Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "" {
		t.Fatalf("expression Call() = %#v, want successful empty result", msg)
	}

	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             "print(1 + 2)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("print Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "3\n" {
		t.Fatalf("print Call() = %#v, want printed value", msg)
	}

	msg, err = callback.Call(t.Context(), map[string]any{
		"code": `print("left", end=" + ")
print("right", end="")`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("custom end Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "left + right" {
		t.Fatalf("custom end Call() = %#v, want custom print endings", msg)
	}
}

func TestStarlarkREPLCallbackPersistsFunctionsAndMutableValues(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code": `items = ["first"]
def add_item(item):
    items.append(item)`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("definition Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != "" {
		t.Fatalf("definition Call() = %#v, want successful empty result", msg)
	}

	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             "add_item(\"second\")\nprint(items)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("reuse Call() error = %v", err)
	}
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != `["first", "second"]`+"\n" {
		t.Fatalf("reuse Call() = %#v, want persisted function and list", msg)
	}
}

func TestStarlarkREPLCallbackCancelsThreadAtExecutionTimeout(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	started := time.Now()
	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             "while True:\n    pass",
		"executionTimeout": 1,
	})
	if err != nil {
		t.Fatalf("Call() error = %v, want recoverable tool result", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("Call() took %v, want thread cancellation near timeout", elapsed)
	}
	if !msg.ToolResultError {
		t.Fatalf("Call() ToolResultError = false, blocks = %#v", msg.Blocks)
	}
	if got := msg.Blocks[0].Content.String(); !strings.Contains(got, "execution timed out after 1s") {
		t.Fatalf("Call() result = %q, want timeout", got)
	}

	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             "print(7)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() after timeout error = %v", err)
	}
	if msg.ToolResultError || msg.Blocks[0].Content.String() != "7\n" {
		t.Fatalf("Call() after timeout = %#v, want uncancelled REPL", msg)
	}
}

func TestStarlarkREPLCallbackRejectsAlreadyCanceledContext(t *testing.T) {
	t.Parallel()

	conn := &recordingACPConn{}
	callback := &starlarkREPLCallback{
		MaxTimeout: 5,
		SessionID:  testSessionID,
		Conn:       conn,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := callback.Call(ctx, map[string]any{
		"code":             "print(1)",
		"executionTimeout": 2,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Call() error = %v, want context.Canceled", err)
	}
	if len(conn.updates) != 0 {
		t.Fatalf("SessionUpdate count = %d, want none for evaluation that did not start", len(conn.updates))
	}
}

func TestStarlarkREPLCallbackCancelsThreadWithParentContext(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	ctx, cancel := context.WithCancel(t.Context())
	cancelTimer := time.AfterFunc(100*time.Millisecond, cancel)
	defer cancelTimer.Stop()

	started := time.Now()
	_, err := callback.Call(ctx, map[string]any{
		"code":             "while True:\n    pass",
		"executionTimeout": 5,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Call() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Call() took %v, want prompt cancellation to stop the thread", elapsed)
	}

	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             "print(8)",
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() after cancellation error = %v", err)
	}
	if msg.ToolResultError || msg.Blocks[0].Content.String() != "8\n" {
		t.Fatalf("Call() after cancellation = %#v, want uncancelled REPL", msg)
	}
}

func TestStarlarkREPLCallbackRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		maxTimeout int
		params     map[string]any
		want       string
	}{
		{
			name:       "malformed parameters",
			maxTimeout: 5,
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": "soon",
			},
			want: "Error parsing parameters:",
		},
		{
			name:       "timeout below minimum",
			maxTimeout: 5,
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 0,
			},
			want: "executionTimeout must be at least 1 second",
		},
		{
			name:       "timeout above configured maximum",
			maxTimeout: 5,
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 6,
			},
			want: "executionTimeout exceeds maximum allowed (5 seconds)",
		},
		{
			name: "timeout above default maximum",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 301,
			},
			want: "executionTimeout exceeds maximum allowed (300 seconds)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callback := &starlarkREPLCallback{MaxTimeout: tt.maxTimeout}
			msg, err := callback.Call(t.Context(), tt.params)
			if err != nil {
				t.Fatalf("Call() error = %v, want recoverable tool result", err)
			}
			if !msg.ToolResultError || len(msg.Blocks) != 1 {
				t.Fatalf("Call() = %#v, want one tool-error block", msg)
			}
			if got := msg.Blocks[0].Content.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("Call() result = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestStarlarkREPLCallbackReturnsExecutionErrorsWithOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want []string
	}{
		{
			name: "syntax error",
			code: "def broken(:\n    pass",
			want: []string{"Starlark execution error:"},
		},
		{
			name: "runtime error retains prior output",
			code: "print(\"before\")\nfail(\"boom\")",
			want: []string{"Starlark execution error:", "boom", "Output:\nbefore\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callback := &starlarkREPLCallback{MaxTimeout: 5}
			msg, err := callback.Call(t.Context(), map[string]any{
				"code":             tt.code,
				"executionTimeout": 2,
			})
			if err != nil {
				t.Fatalf("Call() error = %v, want recoverable tool result", err)
			}
			if !msg.ToolResultError || len(msg.Blocks) != 1 {
				t.Fatalf("Call() = %#v, want one tool-error block", msg)
			}
			got := msg.Blocks[0].Content.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("Call() result = %q, want substring %q", got, want)
				}
			}
		})
	}
}

func TestStarlarkREPLCallbackReturnsSessionUpdateError(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("session update failed")
	conn := &recordingACPConn{err: updateErr}
	callback := &starlarkREPLCallback{
		MaxTimeout: 5,
		SessionID:  testSessionID,
		Conn:       conn,
	}
	toolCallID := acp.ToolCallId("call-update-error")
	_, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), map[string]any{
		"code":             `fail("evaluation must not run")`,
		"executionTimeout": 2,
	})
	if !errors.Is(err, updateErr) {
		t.Fatalf("Call() error = %v, want session update error", err)
	}
	if len(conn.updates) != 1 {
		t.Fatalf("SessionUpdate count = %d, want only in-progress attempt: %#v", len(conn.updates), conn.updates)
	}
	requireExecutionStatus(t, conn.updates[0], toolCallID, acp.ToolCallStatusInProgress)
}

func TestStarlarkREPLCallbackReturnsErrorsFromEveryTerminalSessionUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		params     map[string]any
		failAt     int
		wantStatus acp.ToolCallStatus
	}{
		{
			name: "malformed parameters",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": "soon",
			},
			failAt:     1,
			wantStatus: acp.ToolCallStatusFailed,
		},
		{
			name: "timeout below minimum",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 0,
			},
			failAt:     1,
			wantStatus: acp.ToolCallStatusFailed,
		},
		{
			name: "timeout above maximum",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 6,
			},
			failAt:     1,
			wantStatus: acp.ToolCallStatusFailed,
		},
		{
			name: "execution failure",
			params: map[string]any{
				"code":             `fail("boom")`,
				"executionTimeout": 2,
			},
			failAt:     2,
			wantStatus: acp.ToolCallStatusFailed,
		},
		{
			name: "successful completion",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 2,
			},
			failAt:     2,
			wantStatus: acp.ToolCallStatusCompleted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			updateErr := errors.New("session update failed")
			conn := &recordingACPConn{err: updateErr, errAt: tt.failAt}
			callback := &starlarkREPLCallback{
				MaxTimeout: 5,
				SessionID:  testSessionID,
				Conn:       conn,
			}
			toolCallID := acp.ToolCallId("call-terminal-update-error")
			_, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), tt.params)
			if !errors.Is(err, updateErr) {
				t.Fatalf("Call() error = %v, want session update error", err)
			}
			if len(conn.updates) != tt.failAt {
				t.Fatalf("SessionUpdate count = %d, want %d: %#v", len(conn.updates), tt.failAt, conn.updates)
			}
			requireExecutionStatus(t, conn.updates[len(conn.updates)-1], toolCallID, tt.wantStatus)
		})
	}
}

func TestStarlarkREPLCallbackReportsExecutionStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		params        map[string]any
		wantToolError bool
		wantStatuses  []acp.ToolCallStatus
		wantContent   string
	}{
		{
			name: "successful evaluation",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 2,
			},
			wantStatuses: []acp.ToolCallStatus{acp.ToolCallStatusInProgress, acp.ToolCallStatusCompleted},
			wantContent:  "1\n",
		},
		{
			name: "execution failure",
			params: map[string]any{
				"code":             `fail("boom")`,
				"executionTimeout": 2,
			},
			wantToolError: true,
			wantStatuses:  []acp.ToolCallStatus{acp.ToolCallStatusInProgress, acp.ToolCallStatusFailed},
			wantContent:   "boom",
		},
		{
			name: "validation failure",
			params: map[string]any{
				"code":             "print(1)",
				"executionTimeout": 0,
			},
			wantToolError: true,
			wantStatuses:  []acp.ToolCallStatus{acp.ToolCallStatusFailed},
			wantContent:   "executionTimeout must be at least 1 second",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			conn := &recordingACPConn{}
			callback := &starlarkREPLCallback{
				MaxTimeout: 5,
				SessionID:  testSessionID,
				Conn:       conn,
			}
			toolCallID := acp.ToolCallId("call-1")
			msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), tt.params)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if msg.ToolResultError != tt.wantToolError {
				t.Fatalf("Call() ToolResultError = %t, want %t", msg.ToolResultError, tt.wantToolError)
			}
			if len(conn.updates) != len(tt.wantStatuses) {
				t.Fatalf("SessionUpdate count = %d, want %d: %#v", len(conn.updates), len(tt.wantStatuses), conn.updates)
			}
			for i, wantStatus := range tt.wantStatuses {
				requireExecutionStatus(t, conn.updates[i], toolCallID, wantStatus)
			}
			content, ok := conn.updates[len(conn.updates)-1].Update.Content.([]acp.ToolCallContent)
			if !ok || len(content) != 1 || !strings.Contains(content[0].Content.Text, tt.wantContent) {
				t.Fatalf("final tool content = %#v, want text containing %q", conn.updates[len(conn.updates)-1].Update.Content, tt.wantContent)
			}
		})
	}
}

func TestStarlarkREPLCallbackReportsMultimodalResultInOneCompletedUpdate(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	artifacts := []struct {
		name string
		data []byte
	}{
		{name: "image.png", data: []byte("image-data")},
		{name: "report.pdf", data: []byte("pdf-data")},
		{name: "audio.wav", data: []byte("audio-data")},
		{name: "video.mp4", data: []byte("video-data")},
	}
	for _, artifact := range artifacts {
		if err := os.WriteFile(filepath.Join(cwd, artifact.name), artifact.data, 0o600); err != nil {
			t.Fatalf("write %s: %v", artifact.name, err)
		}
	}

	conn := &recordingACPConn{}
	callback := &starlarkREPLCallback{
		Cwd:        cwd,
		MaxTimeout: 5,
		SessionID:  testSessionID,
		Conn:       conn,
	}
	toolCallID := acp.ToolCallId("call-multimodal")
	msg, err := callback.Call(xctx.WithToolCallId(t.Context(), toolCallID), map[string]any{
		"code": `print("artifacts")
view_file("image.png", mime_type="image/png")
view_file("report.pdf", mime_type="application/pdf")
view_file("audio.wav", mime_type="audio/wav")
view_file("video.mp4", mime_type="video/mp4")`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if msg.ToolResultError {
		t.Fatalf("Call() returned tool error: %#v", msg)
	}
	if len(conn.updates) != 2 {
		t.Fatalf("SessionUpdate count = %d, want in-progress and completed updates: %#v", len(conn.updates), conn.updates)
	}
	requireExecutionStatus(t, conn.updates[0], toolCallID, acp.ToolCallStatusInProgress)
	requireExecutionStatus(t, conn.updates[1], toolCallID, acp.ToolCallStatusCompleted)

	pdfResource := acp.BlobResourceContentsEmbeddedResourceResource(
		base64.StdEncoding.EncodeToString([]byte("pdf-data")),
		"artifact:///report.pdf",
	)
	pdfResource.MimeType = new("application/pdf")
	videoResource := acp.BlobResourceContentsEmbeddedResourceResource(
		base64.StdEncoding.EncodeToString([]byte("video-data")),
		"artifact:///video.mp4",
	)
	videoResource.MimeType = new("video/mp4")
	want := []acp.ToolCallContent{
		acp.ContentToolCallContent(acp.TextContentBlock("artifacts\n")),
		acp.ContentToolCallContent(acp.ImageContentBlock(base64.StdEncoding.EncodeToString([]byte("image-data")), "image/png")),
		acp.ContentToolCallContent(acp.ResourceContentBlock(pdfResource)),
		acp.ContentToolCallContent(acp.AudioContentBlock(base64.StdEncoding.EncodeToString([]byte("audio-data")), "audio/wav")),
		acp.ContentToolCallContent(acp.ResourceContentBlock(videoResource)),
	}
	got, ok := conn.updates[1].Update.Content.([]acp.ToolCallContent)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("completed content = %#v, want %#v", conn.updates[1].Update.Content, want)
	}
}

func TestStarlarkREPLCallbackLoadsDysonModulesAndGlobals(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code": `load("re.star", "re")
load("json.star", "json")
load("acp.star", "acp")
print(dir(acp))
print(json.dumps({"b": [2], "a": True}))`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("load Call() error = %v", err)
	}
	wantLoadOutput := "[\"get_session\", \"list_sessions\"]\n{\"a\":true,\"b\":[2]}\n"
	if msg.ToolResultError || len(msg.Blocks) != 1 || msg.Blocks[0].Content.String() != wantLoadOutput {
		t.Fatalf("load Call() = %#v, want registered and functional Dyson and ACP modules", msg)
	}

	msg, err = callback.Call(t.Context(), map[string]any{
		"code":             `print(re.findall("[0-9]+", "a12 b34"))`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("module reuse Call() error = %v", err)
	}
	if msg.ToolResultError {
		t.Fatalf("module reuse Call() returned tool error: %s", msg.Blocks[0].Content.String())
	}
	if got := msg.Blocks[0].Content.String(); got != `["12", "34"]`+"\n" {
		t.Fatalf("module reuse Call() output = %q, want Dyson re module result", got)
	}
}

func TestStarlarkREPLCallbackViewFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filename     string
		data         []byte
		providedMIME string
		wantMIME     string
		wantModality gai.Modality
	}{
		{name: "image with inferred extension MIME", filename: "image.png", wantMIME: "image/png", wantModality: gai.Image},
		{name: "image with content-sniffed MIME", filename: "image", data: []byte("\x89PNG\r\n\x1a\ncontents"), wantMIME: "image/png", wantModality: gai.Image},
		{name: "PDF", filename: "report.bin", providedMIME: "application/pdf", wantMIME: "application/pdf", wantModality: gai.Image},
		{name: "legacy PDF MIME is normalized", filename: "legacy.bin", providedMIME: "application/x-pdf", wantMIME: "application/pdf", wantModality: gai.Image},
		{name: "audio", filename: "audio.bin", providedMIME: "audio/wav", wantMIME: "audio/wav", wantModality: gai.Audio},
		{name: "video", filename: "video.bin", providedMIME: "video/mp4", wantMIME: "video/mp4", wantModality: gai.Video},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cwd := t.TempDir()
			data := tt.data
			if data == nil {
				data = []byte("binary-" + tt.name)
			}
			if err := os.WriteFile(filepath.Join(cwd, tt.filename), data, 0o600); err != nil {
				t.Fatalf("write artifact: %v", err)
			}
			callback := &starlarkREPLCallback{Cwd: cwd, MaxTimeout: 5}
			msg, err := callback.Call(t.Context(), map[string]any{
				"code": fmt.Sprintf(
					"view_file(%q, mime_type=%q)",
					tt.filename,
					tt.providedMIME,
				),
				"executionTimeout": 2,
			})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if msg.ToolResultError || len(msg.Blocks) != 1 {
				t.Fatalf("Call() = %#v, want one media block", msg)
			}
			block := msg.Blocks[0]
			if block.ModalityType != tt.wantModality || block.MimeType != tt.wantMIME {
				t.Fatalf("media block = %#v, want modality %v and MIME %q", block, tt.wantModality, tt.wantMIME)
			}
			if got := block.Content.String(); got != base64.StdEncoding.EncodeToString(data) {
				t.Fatalf("media content = %q, want encoded artifact", got)
			}
			if got := block.ExtraFields[gai.BlockFieldFilenameKey]; got != tt.filename {
				t.Fatalf("media filename = %#v, want %q", got, tt.filename)
			}
		})
	}
}

func TestStarlarkREPLCallbackViewFileErrors(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "note.txt"), []byte("plain text"), 0o600); err != nil {
		t.Fatalf("write text artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "artifact.bin"), []byte("contents"), 0o600); err != nil {
		t.Fatalf("write binary artifact: %v", err)
	}

	tests := []struct {
		name string
		code string
		want string
	}{
		{name: "missing path argument", code: "view_file()", want: "missing argument for path"},
		{name: "empty path", code: `view_file("")`, want: "path must not be empty"},
		{name: "missing file", code: `view_file("missing.png")`, want: "missing.png"},
		{name: "unsupported MIME", code: `view_file("note.txt")`, want: "unsupported MIME type"},
		{name: "invalid MIME", code: `view_file("artifact.bin", mime_type="not a mime")`, want: "invalid MIME type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callback := &starlarkREPLCallback{Cwd: cwd, MaxTimeout: 5}
			msg, err := callback.Call(t.Context(), map[string]any{
				"code":             tt.code,
				"executionTimeout": 2,
			})
			if err != nil {
				t.Fatalf("Call() error = %v, want recoverable tool result", err)
			}
			if !msg.ToolResultError || len(msg.Blocks) != 1 {
				t.Fatalf("Call() = %#v, want one tool-error block", msg)
			}
			if got := msg.Blocks[0].Content.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("Call() result = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestStarlarkREPLCallbackTruncatesPrintOutput(t *testing.T) {
	t.Parallel()

	callback := &starlarkREPLCallback{MaxTimeout: 5, LargeOutputCharLimit: 10}
	msg, err := callback.Call(t.Context(), map[string]any{
		"code":             `print("abcdefghijklmnopqrstuvwxyz")`,
		"executionTimeout": 2,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	got := msg.Blocks[0].Content.String()
	if !strings.Contains(got, "NOTE: output beginning was truncated") {
		t.Fatalf("Call() output = %q, want truncation note", got)
	}
	if !strings.HasSuffix(got, "rstuvwxyz\n") {
		t.Fatalf("Call() output = %q, want retained tail", got)
	}
}

func TestMakeToolUsesStarlarkREPLContract(t *testing.T) {
	t.Parallel()

	tool := makeTool(42)
	if tool.Name != "starlark_repl" {
		t.Fatalf("MakeTool().Name = %q", tool.Name)
	}
	if !strings.Contains(tool.Description, "session-scoped REPL") {
		t.Fatalf("MakeTool().Description does not describe REPL: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "Use `print(...)`") {
		t.Fatalf("MakeTool().Description does not explain printed output: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "view_file") {
		t.Fatalf("MakeTool().Description does not document view_file: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "requests.star") {
		t.Fatalf("MakeTool().Description does not document HTTP requests: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "global `open") {
		t.Fatalf("MakeTool().Description does not document open: %q", tool.Description)
	}
	if tool.InputSchema == nil || tool.InputSchema.Type != "object" {
		t.Fatalf("MakeTool().InputSchema = %#v, want object schema", tool.InputSchema)
	}
	if !slices.Equal(tool.InputSchema.Required, []string{"code", "executionTimeout"}) {
		t.Fatalf("MakeTool().InputSchema.Required = %#v", tool.InputSchema.Required)
	}
	if code := tool.InputSchema.Properties["code"]; code == nil || code.Type != "string" {
		t.Fatalf("code schema = %#v, want string", code)
	}
	timeout := tool.InputSchema.Properties["executionTimeout"]
	if timeout == nil || timeout.Type != "integer" || timeout.Minimum == nil || *timeout.Minimum != 1 || timeout.Maximum == nil || *timeout.Maximum != 42 {
		t.Fatalf("executionTimeout schema = %#v, want integer range 1-42", timeout)
	}

	defaultTimeout := makeTool(0).InputSchema.Properties["executionTimeout"]
	if defaultTimeout.Maximum == nil || *defaultTimeout.Maximum != 300 {
		t.Fatalf("default executionTimeout maximum = %#v, want 300", defaultTimeout.Maximum)
	}
}
