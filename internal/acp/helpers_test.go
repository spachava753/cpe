package acp

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
)

type noOpAcpClient struct{}

type runtimeCreatorFunc func(context.Context, session, acp.ClientCapabilities, *acp.AgentConnection) (runtime, error)

func (f runtimeCreatorFunc) Create(ctx context.Context, s session, caps acp.ClientCapabilities) (runtime, error) {
	return f(ctx, s, caps, nil)
}

type runtimeCreatorAdapter struct {
	f    runtimeCreatorFunc
	conn *acp.AgentConnection
}

func (r *runtimeCreatorAdapter) Create(ctx context.Context, s session, caps acp.ClientCapabilities) (runtime, error) {
	return r.f(ctx, s, caps, r.conn)
}

var unreachableRuntimeFactory = runtimeCreatorFunc(func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
	panic("should not be called")
})

type testSetup struct {
	ClientConn *acp.Client
	AgentConn  *acp.AgentConnection
	Store      *storage.Sqlite
	RawDB      *sql.DB
}

func expectedUsageUpdate(used, size uint64, cost *acp.Cost) acp.SessionUpdate {
	update := acp.UsageUpdateSessionUpdate(used, size)
	update.Cost = cost
	return update
}

func expectedRPCUserMessageChunk(text string) acp.SessionUpdate {
	update := acp.UserMessageChunkSessionUpdate(acp.TextContentBlock(text))
	update.Content = map[string]any{"text": text, "type": "text"}
	return update
}

func expectedRPCAgentThoughtChunk(text string) acp.SessionUpdate {
	update := acp.AgentThoughtChunkSessionUpdate(acp.TextContentBlock(text))
	update.Content = map[string]any{"text": text, "type": "text"}
	return update
}

func expectedRPCAgentMessageChunk(text string) acp.SessionUpdate {
	update := acp.AgentMessageChunkSessionUpdate(acp.TextContentBlock(text))
	update.Content = map[string]any{"text": text, "type": "text"}
	return update
}

func expectedPendingToolCallUpdate(id acp.ToolCallId, title string, rawInput any) acp.SessionUpdate {
	status := acp.ToolCallStatusPending
	update := acp.ToolCallSessionUpdate(id, title)
	update.RawInput = rawInput
	update.Status = &status
	return update
}

func setup(
	t *testing.T,
	client any,
	cfg *config.RawConfig,
	rf runtimeCreatorFunc,
) testSetup {
	t.Helper()

	// setup db
	db, err := sql.Open("sqlite3", ":memory:")
	be.Err(t, err, nil)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	sqliteStorage, err := storage.NewSqlite(t.Context(), db)
	be.Err(t, err, nil)

	adapter := &runtimeCreatorAdapter{f: rf}
	ag := Agent{
		activeSessions: new(sync.Map[acp.SessionId, *sync.Guard[session]]),
		genId: func() acp.SessionId {
			return acp.SessionId(storage.GenerateId())
		},
		runtimeFactory: adapter,
		rawCfg:         cfg,
		skillHomeDir:   t.TempDir(),
		db:             sqliteStorage,
	}

	clientTransport, agentTransport := acp.NewInMemoryTransports()
	agentCtx, cancelAgent := context.WithCancel(t.Context())
	agentDone := make(chan error, 1)
	go func() {
		agentDone <- acp.RunAgent(agentCtx, agentTransport, func(conn *acp.AgentConnection) any {
			ag.conn = conn
			adapter.conn = conn
			return &ag
		})
	}()
	t.Cleanup(func() {
		cancelAgent()
		select {
		case <-agentDone:
		case <-time.After(time.Second):
			t.Log("timed out waiting for test ACP agent to exit")
		}
	})
	clientConn, err := acp.Connect(t.Context(), clientTransport, client)
	be.Err(t, err, nil)
	t.Cleanup(func() { _ = clientConn.Close() })

	t.Log("created connection")
	return testSetup{
		ClientConn: clientConn,
		AgentConn:  adapter.conn,
		Store:      sqliteStorage,
		RawDB:      db,
	}
}
