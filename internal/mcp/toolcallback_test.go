package mcp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/acp/xctx"
	"github.com/spachava753/cpe/internal/mcpconfig"
)

const testSessionID acp.SessionId = "session-1"

type recordingSessionUpdator struct {
	updates []acp.SessionNotification
}

func (r *recordingSessionUpdator) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	r.updates = append(r.updates, params)
	return nil
}

var _ sessionUpdator = (*recordingSessionUpdator)(nil)

func newToolCallbackTestSession(t *testing.T, result *mcpsdk.CallToolResult, handlerErr error) *mcpsdk.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "test-server", Version: "test"}, nil)
	server.AddTool(&mcpsdk.Tool{
		Name:        "lookup",
		Description: "Look up test data.",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return result, handlerErr
	})

	serverCtx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(serverCtx, serverTransport)
	}()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Log("timed out waiting for test MCP server to exit")
		}
	})
	return session
}

func requireToolCallUpdate(t *testing.T, got acp.SessionNotification, wantID acp.ToolCallId, wantStatus acp.ToolCallStatus) *acp.SessionToolCallUpdate {
	t.Helper()

	if got.SessionId != testSessionID {
		t.Fatalf("SessionId = %q, want %q", got.SessionId, testSessionID)
	}
	if got.Update.ToolCallUpdate == nil {
		t.Fatalf("ToolCallUpdate is nil in %#v", got.Update)
	}
	update := got.Update.ToolCallUpdate
	if update.ToolCallId != wantID {
		t.Fatalf("ToolCallId = %q, want %q", update.ToolCallId, wantID)
	}
	if update.Status == nil || *update.Status != wantStatus {
		t.Fatalf("Status = %#v, want %q", update.Status, wantStatus)
	}
	return update
}

func requireToolResultText(t *testing.T, msg gai.Message, wantError bool) string {
	t.Helper()

	if msg.Role != gai.ToolResult {
		t.Fatalf("Role = %q, want %q", msg.Role, gai.ToolResult)
	}
	if msg.ToolResultError != wantError {
		t.Fatalf("ToolResultError = %t, want %t", msg.ToolResultError, wantError)
	}
	if len(msg.Blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1: %#v", len(msg.Blocks), msg.Blocks)
	}
	return msg.Blocks[0].Content.String()
}

func requireToolCallContentText(t *testing.T, update *acp.SessionToolCallUpdate) string {
	t.Helper()

	if len(update.Content) != 1 || update.Content[0].Content == nil || update.Content[0].Content.Content.Text == nil {
		t.Fatalf("tool call content = %#v, want one text content", update.Content)
	}
	return update.Content[0].Content.Content.Text.Text
}

func requireText(t *testing.T, label, got, want string, contains bool) {
	t.Helper()

	if contains {
		if !strings.Contains(got, want) {
			t.Fatalf("%s = %q, want containing %q", label, got, want)
		}
		return
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
}

func TestToolCallbackReportsSessionUpdates(t *testing.T) {
	t.Parallel()

	params := map[string]any{"query": "docs"}
	tests := []struct {
		name                string
		result              *mcpsdk.CallToolResult
		handlerErr          error
		toolCallID          acp.ToolCallId
		wantToolResultError bool
		wantFinalStatus     acp.ToolCallStatus
		wantText            string
		wantTextContains    bool
	}{
		{
			name: "success",
			result: &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lookup result"}},
			},
			toolCallID:      "call-success",
			wantFinalStatus: acp.ToolCallStatusCompleted,
			wantText:        "lookup result",
		},
		{
			name: "tool error",
			result: &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "lookup failed"}},
				IsError: true,
			},
			toolCallID:          "call-tool-error",
			wantToolResultError: true,
			wantFinalStatus:     acp.ToolCallStatusFailed,
			wantText:            "lookup failed",
		},
		{
			name:             "call error",
			handlerErr:       fmt.Errorf("server exploded"),
			toolCallID:       "call-call-error",
			wantFinalStatus:  acp.ToolCallStatusFailed,
			wantText:         "server exploded",
			wantTextContains: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := newToolCallbackTestSession(t, tt.result, tt.handlerErr)
			updator := &recordingSessionUpdator{}
			callback := NewToolCallback(updator, testSessionID, session, "server", "lookup", mcpconfig.ServerConfig{})

			msg, err := callback.Call(xctx.WithToolCallId(t.Context(), tt.toolCallID), params)
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			requireText(t, "tool result text", requireToolResultText(t, msg, tt.wantToolResultError), tt.wantText, tt.wantTextContains)

			if len(updator.updates) != 2 {
				t.Fatalf("updates len = %d, want 2: %#v", len(updator.updates), updator.updates)
			}
			inProgress := requireToolCallUpdate(t, updator.updates[0], tt.toolCallID, acp.ToolCallStatusInProgress)
			if inProgress.Kind == nil || *inProgress.Kind != acp.ToolKindOther {
				t.Fatalf("Kind = %#v, want %q", inProgress.Kind, acp.ToolKindOther)
			}
			if !reflect.DeepEqual(inProgress.RawInput, params) {
				t.Fatalf("RawInput = %#v, want %#v", inProgress.RawInput, params)
			}

			final := requireToolCallUpdate(t, updator.updates[1], tt.toolCallID, tt.wantFinalStatus)
			requireText(t, "final update text", requireToolCallContentText(t, final), tt.wantText, tt.wantTextContains)
		})
	}
}
