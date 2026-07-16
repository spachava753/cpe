package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	cpelogging "github.com/spachava753/cpe/internal/logging"
	"github.com/spachava753/cpe/internal/storage"
	cpesync "github.com/spachava753/cpe/internal/sync"
)

type contextCapturingRuntime struct {
	captured *context.Context
}

func (r contextCapturingRuntime) Generate(ctx context.Context, _ gai.Dialog, _ *gai.GenOpts) (gai.Dialog, error) {
	*r.captured = ctx
	return nil, context.Canceled
}

func (contextCapturingRuntime) Close() error {
	return nil
}

func TestPromptScopesRuntimeLogsToSession(t *testing.T) {
	store, _ := newTestSqlite(t)
	sessionID := acp.SessionId("session-1")
	cwd := t.TempDir()
	if err := store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acp.SessionInfo{
			Cwd:       cwd,
			SessionID: sessionID,
		},
		ModelRef: "test-model",
	}); err != nil {
		t.Fatalf("create ACP session: %v", err)
	}

	activeSessions := new(cpesync.Map[acp.SessionId, *cpesync.Guard[session]])
	activeSessions.Store(sessionID, cpesync.NewGuard(session{
		id:    sessionID,
		cwd:   cwd,
		model: "test-model",
	}))
	var runtimeCtx context.Context
	agent := Agent{
		activeSessions: activeSessions,
		runtimeFactory: runtimeCreatorFunc(func(context.Context, session, acp.ClientCapabilities, *acp.AgentConnection) (runtime, error) {
			return contextCapturingRuntime{captured: &runtimeCtx}, nil
		}),
		skillHomeDir: t.TempDir(),
		db:           store,
	}

	response, err := agent.Prompt(t.Context(), &acp.PromptRequest{
		SessionID: sessionID,
		Prompt:    []acp.ContentBlock{acp.TextContentBlock("hello")},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if response.StopReason != acp.StopReasonCancelled {
		t.Fatalf("Prompt() stop reason = %q, want %q", response.StopReason, acp.StopReasonCancelled)
	}
	if runtimeCtx == nil {
		t.Fatal("runtime did not receive prompt context")
	}

	var output bytes.Buffer
	logger := slog.New(cpelogging.NewContextHandler(slog.NewJSONHandler(&output, nil)))
	logger.InfoContext(runtimeCtx, "runtime log")
	var record struct {
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode runtime log: %v", err)
	}
	if record.SessionID != string(sessionID) || record.Cwd != cwd {
		t.Fatalf("runtime log scope = %#v, want session_id %q and cwd %q", record, sessionID, cwd)
	}
}
