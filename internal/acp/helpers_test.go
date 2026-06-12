package acp

import (
	"context"
	"database/sql"
	"io"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
)

type noOpAcpClient struct{}

// CreateTerminal implements [acp.Client].
func (t *noOpAcpClient) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	panic("unimplemented")
}

// KillTerminal implements [acp.Client].
func (t *noOpAcpClient) KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	panic("unimplemented")
}

// ReadTextFile implements [acp.Client].
func (t *noOpAcpClient) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	panic("unimplemented")
}

// ReleaseTerminal implements [acp.Client].
func (t *noOpAcpClient) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	panic("unimplemented")
}

// RequestPermission implements [acp.Client].
func (t *noOpAcpClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	panic("unimplemented")
}

// SessionUpdate implements [acp.Client].
func (t *noOpAcpClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	panic("unimplemented")
}

// TerminalOutput implements [acp.Client].
func (t *noOpAcpClient) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	panic("unimplemented")
}

// WaitForTerminalExit implements [acp.Client].
func (t *noOpAcpClient) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	panic("unimplemented")
}

// WriteTextFile implements [acp.Client].
func (t *noOpAcpClient) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	panic("unimplemented")
}

var _ acp.Client = (*noOpAcpClient)(nil)

type runtimeCreatorFunc func(context.Context, runtimeOpts) (runtime, error)

func (f runtimeCreatorFunc) Create(ctx context.Context, opts runtimeOpts) (runtime, error) {
	return f(ctx, opts)
}

var unreachableRuntimeFactory = runtimeCreatorFunc(func(ctx context.Context, opts runtimeOpts) (runtime, error) {
	panic("should not be called")
})

type testSetup struct {
	ClientConn *acp.ClientSideConnection
	AgentConn  *acp.AgentSideConnection
	Store      *storage.Sqlite
	RawDB      *sql.DB
}

func setup(
	t *testing.T,
	client acp.Client,
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

	// setup client agent connection
	ar, aw := io.Pipe()
	cr, cw := io.Pipe()

	ag := Agent{
		activeSessions: new(sync.Map[acp.SessionId, *sync.Guard[session]]),
		genId: func() acp.SessionId {
			return acp.SessionId(storage.GenerateId())
		},
		runtimeFactory: rf,
		rawCfg:         cfg,
		db:             sqliteStorage,
	}
	asc := acp.NewAgentSideConnection(&ag, aw, cr)
	ag.conn = asc
	go func() {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(func() {
			cancel()
		})
		select {
		case <-asc.Done():
		case <-ctx.Done():
		}
	}()
	t.Log("started agent")

	clientConn := acp.NewClientSideConnection(client, cw, ar)
	t.Log("created connection")
	return testSetup{
		ClientConn: clientConn,
		AgentConn:  asc,
		Store:      sqliteStorage,
		RawDB:      db,
	}
}
