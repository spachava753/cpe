package acp

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/sync"
	"github.com/spachava753/gai"
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

// mockRuntime is used to simulate a [acpRuntime]. It needs to be able to return a response, or an error, and be able to simulate work
type mockRuntime func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error)

// Close implements [acpRuntime].
func (m mockRuntime) Close() error {
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
	client acp.Client,
	cfg *config.RawConfig,
	rf runtimeFactory,
) (*acp.ClientSideConnection, *acp.AgentSideConnection, *storage.Sqlite) {
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

	var asc *acp.AgentSideConnection
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
		asc = acp.NewAgentSideConnection(&ag, aw, cr)
		ag.conn = asc
		select {
		case <-asc.Done():
		case <-ctx.Done():
		}
	}()
	t.Log("started agent")

	clientConn := acp.NewClientSideConnection(client, cw, ar)
	t.Log("created connection")
	return clientConn, asc, sqliteStorage
}

func TestInit(t *testing.T) {
	clientConn, _, _ := setup(t, &noOpAcpClient{}, &config.RawConfig{}, unreachableRuntimeFactory)

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
	clientConn, _, store := setup(t, &noOpAcpClient{}, &config.RawConfig{}, unreachableRuntimeFactory)

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

type promptTestClient struct {
	noOpAcpClient
	capturedNotifications []acp.SessionNotification
}

// SessionUpdate implements [acp.Client].
func (t *promptTestClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	t.capturedNotifications = append(t.capturedNotifications, params)
	return nil
}

func TestPrompt(t *testing.T) {
	var (
		clientConn *acp.ClientSideConnection
		store      *storage.Sqlite
		cwd        = t.TempDir()
		testClient = &promptTestClient{}
	)
	clientConn, _, store = setup(
		t,
		testClient,
		&config.RawConfig{
			Models: []config.ModelConfig{
				{
					Model: config.Model{
						Ref:                  "test-model",
						DisplayName:          "Test Model",
						ID:                   "test-model",
						Type:                 "responses",
						BaseUrl:              "https://customurl.com/v1",
						ContextWindow:        100,
						InputCostPerMillion:  new(1.0),
						OutputCostPerMillion: new(1.0),
					},
				},
				{
					Model: config.Model{
						Ref:                  "test-model2",
						DisplayName:          "Test Model 2",
						ID:                   "test-model2",
						Type:                 "responses",
						BaseUrl:              "https://customurl.com/v1",
						ContextWindow:        100,
						InputCostPerMillion:  new(1.0),
						OutputCostPerMillion: new(1.0),
					},
				},
			},
		},
		func(conn *acp.AgentSideConnection, modelRef string) (acpRuntime, error) {
			return mockRuntime(func(ctx context.Context, input gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
				generatedDialog := gai.Dialog{
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							{
								ID:           "",
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								MimeType:     "text/plain",
								Content:      gai.Str("let me think"),
								ExtraFields:  map[string]any{},
							},
							{
								ID:           "",
								BlockType:    gai.Content,
								ModalityType: gai.Text,
								MimeType:     "text/plain",
								Content:      gai.Str("here is the answer:"),
								ExtraFields:  map[string]any{},
							},
						},
						ToolResultError: false,
						ExtraFields:     map[string]any{},
					},
				}

				savedDialog := make(gai.Dialog, 0, len(generatedDialog))
				for msg, err := range store.SaveDialog(ctx, slices.Values(generatedDialog)) {
					if err != nil {
						return generatedDialog, err
					}
					savedDialog = append(savedDialog, msg)
				}

				sessionID, ok := ctx.Value(sessionIDCtxKey{}).(acp.SessionId)
				if !ok || sessionID == "" {
					return generatedDialog, errors.New("missing ACP session id")
				}

				for _, m := range generatedDialog {
					for update := range msgToSessionUpdate(m) {
						conn.SessionUpdate(ctx, acp.SessionNotification{
							SessionId: sessionID,
							Update:    update,
						})
					}
				}

				return generatedDialog, nil
			}), nil
		},
	)

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
	be.Err(t, err, nil) // we should not get an error on init connection

	// new session
	newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	be.Err(t, err, nil)
	sessionId := newSessionResp.SessionId
	be.True(t, sessionId != "") // session id cannot be empty
	be.Equal(t, newSessionResp.ConfigOptions, []acp.SessionConfigOption{
		{
			Select: &acp.SessionConfigOptionSelect{
				Category:     new(acp.SessionConfigOptionCategoryModel),
				CurrentValue: acp.SessionConfigValueId("test-model"),
				Description:  new("Choose model"),
				Id:           modelRefConfigId,
				Name:         "Model",
				Options: acp.SessionConfigSelectOptions{
					Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
						{
							Description: new(`Type: responses
Base Url: https://customurl.com/v1
Context Window: 100
Input Cost: 1.00
Output Cost: 1.00`),
							Name:  "Test Model",
							Value: "test-model",
						},
						{
							Description: new(`Type: responses
Base Url: https://customurl.com/v1
Context Window: 100
Input Cost: 1.00
Output Cost: 1.00`),
							Name:  "Test Model 2",
							Value: "test-model2",
						},
					},
				},
				Type: "select",
			},
		},
	})
	// set config option
	_, err = clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			ConfigId:  modelRefConfigId,
			SessionId: sessionId,
			Value:     "test-model",
		},
	})
	be.Err(t, err, nil)
	// prompt
	promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
		Prompt: []acp.ContentBlock{
			{
				Text: &acp.ContentBlockText{
					Text: "Hello",
					Type: "text",
				},
			},
		},
		SessionId: sessionId,
	})
	be.Err(t, err, nil)
	be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
	be.Equal(t, testClient.capturedNotifications, []acp.SessionNotification{
		{
			SessionId: sessionId,
			Update: acp.SessionUpdate{
				AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{
							Text: "let me think",
							Type: "text",
						},
					},
					SessionUpdate: "agent_thought_chunk",
				},
			},
		},
		{
			SessionId: sessionId,
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{
							Text: "here is the answer:",
							Type: "text",
						},
					},
					SessionUpdate: "agent_message_chunk",
				},
			},
		},
	})
}
