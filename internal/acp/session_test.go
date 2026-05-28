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
	"github.com/spachava753/gai"
)

type testAcpClient struct{}

// CreateTerminal implements [acp.Client].
func (t *testAcpClient) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	panic("unimplemented")
}

// KillTerminal implements [acp.Client].
func (t *testAcpClient) KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	panic("unimplemented")
}

// ReadTextFile implements [acp.Client].
func (t *testAcpClient) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	panic("unimplemented")
}

// ReleaseTerminal implements [acp.Client].
func (t *testAcpClient) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	panic("unimplemented")
}

// RequestPermission implements [acp.Client].
func (t *testAcpClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	panic("unimplemented")
}

// SessionUpdate implements [acp.Client].
func (t *testAcpClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	panic("unimplemented")
}

// TerminalOutput implements [acp.Client].
func (t *testAcpClient) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	panic("unimplemented")
}

// WaitForTerminalExit implements [acp.Client].
func (t *testAcpClient) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	panic("unimplemented")
}

// WriteTextFile implements [acp.Client].
func (t *testAcpClient) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	panic("unimplemented")
}

var _ acp.Client = (*testAcpClient)(nil)

// mockRuntime is used ti simulate a [acpRuntime]. It needs to be able to return a response, or an error, and be able to simulate work
type mockRuntime func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error)

// Close implements [acpRuntime].
func (m *mockRuntime) Close() error {
	return nil
}

// Generate implements [acpRuntime].
func (m mockRuntime) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
	return m(ctx, dialog, opts)
}

var _ acpRuntime = (*mockRuntime)(nil)

var unreachableRuntimeFactory = func(conn *acp.AgentSideConnection, modelRef string) (acpRuntime, error) {
	panic("should not be called")
}

func setup(
	t *testing.T,
	cfg *config.RawConfig,
	rf runtimeFactory,
) (*acp.ClientSideConnection, *storage.Sqlite) {
	t.Helper()

	// setup db
	db, err := sql.Open("sqlite3", ":memory:")
	be.Err(t, err, nil)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	sqliteStorage, err := storage.NewSqlite(t.Context(), db)
	be.Err(t, err, nil)

	// setup client agent connection
	client := testAcpClient{}
	ar, aw := io.Pipe()
	cr, cw := io.Pipe()

	go func() {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(func() {
			cancel()
		})
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
		select {
		case <-asc.Done():
		case <-ctx.Done():
		}
	}()
	t.Log("started agent")

	clientConn := acp.NewClientSideConnection(&client, cw, ar)
	t.Log("created connection")
	return clientConn, sqliteStorage
}

func TestInit(t *testing.T) {
	clientConn, _ := setup(t, &config.RawConfig{}, unreachableRuntimeFactory)

	resp, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: false,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersionNumber,
	})
	t.Log("called init")
	// we should not get an error on init connection
	be.Err(t, err, nil)
	// assert agent capabilities
	be.True(t, resp.AgentCapabilities.LoadSession)
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Close, &acp.SessionCloseCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.List, &acp.SessionListCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Resume, &acp.SessionResumeCapabilities{})
	be.True(t, resp.AgentCapabilities.PromptCapabilities.Audio)
	be.True(t, resp.AgentCapabilities.PromptCapabilities.Image)
	be.True(t, !resp.AgentCapabilities.PromptCapabilities.EmbeddedContext)
}

func TestListSessions(t *testing.T) {
	clientConn, store := setup(t, &config.RawConfig{}, unreachableRuntimeFactory)

	// seed the db
	sessionEntries := []storage.CreateACPSessionParams{
		{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionId: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "gpt-5.5",
			ThinkingLevel: "low",
		},
		{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir2",
				SessionId: "123abc",
			},
			LastMessageID: "",
			ModelRef:      "gpt-5.4-mini",
			ThinkingLevel: "xhigh",
		},
	}
	for _, se := range sessionEntries {
		be.Err(t, store.CreateACPSession(t.Context(), se), nil)
	}

	_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: false,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersionNumber,
	})
	t.Log("called init")
	// we should not get an error on init connection
	be.Err(t, err, nil)

	// TODO: we should assert the order as well, as the order returned
	// will be based on most recent acp session first and descendng
	resp, err := clientConn.ListSessions(t.Context(), acp.ListSessionsRequest{})
	be.Err(t, err, nil)
	be.Equal(t, len(resp.Sessions), len(sessionEntries))
}
