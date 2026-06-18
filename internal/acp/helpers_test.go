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

// CreateTerminal implements [acp.TerminalClientHandler].
func (t *noOpAcpClient) CreateTerminal(ctx context.Context, params *acp.CreateTerminalRequest) (*acp.CreateTerminalResponse, error) {
	panic("unimplemented")
}

// KillTerminal implements [acp.TerminalClientHandler].
func (t *noOpAcpClient) KillTerminal(ctx context.Context, params *acp.KillTerminalRequest) (*acp.KillTerminalResponse, error) {
	panic("unimplemented")
}

// ReadTextFile implements [acp.FsClientHandler].
func (t *noOpAcpClient) ReadTextFile(ctx context.Context, params *acp.ReadTextFileRequest) (*acp.ReadTextFileResponse, error) {
	panic("unimplemented")
}

// ReleaseTerminal implements [acp.TerminalClientHandler].
func (t *noOpAcpClient) ReleaseTerminal(ctx context.Context, params *acp.ReleaseTerminalRequest) (*acp.ReleaseTerminalResponse, error) {
	panic("unimplemented")
}

// RequestPermission implements [acp.SessionClientHandler].
func (t *noOpAcpClient) RequestPermission(ctx context.Context, params *acp.RequestPermissionRequest) (*acp.RequestPermissionResponse, error) {
	panic("unimplemented")
}

// Update implements [acp.SessionClientHandler].
func (t *noOpAcpClient) Update(ctx context.Context, params *acp.SessionNotification) error {
	panic("unimplemented")
}

// TerminalOutput implements [acp.TerminalClientHandler].
func (t *noOpAcpClient) TerminalOutput(ctx context.Context, params *acp.TerminalOutputRequest) (*acp.TerminalOutputResponse, error) {
	panic("unimplemented")
}

// WaitForTerminalExit implements [acp.TerminalClientHandler].
func (t *noOpAcpClient) WaitForTerminalExit(ctx context.Context, params *acp.WaitForTerminalExitRequest) (*acp.WaitForTerminalExitResponse, error) {
	panic("unimplemented")
}

// WriteTextFile implements [acp.FsClientHandler].
func (t *noOpAcpClient) WriteTextFile(ctx context.Context, params *acp.WriteTextFileRequest) (*acp.WriteTextFileResponse, error) {
	panic("unimplemented")
}

var _ acp.SessionClientHandler = (*noOpAcpClient)(nil)
var _ acp.TerminalClientHandler = (*noOpAcpClient)(nil)
var _ acp.FsClientHandler = (*noOpAcpClient)(nil)

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
