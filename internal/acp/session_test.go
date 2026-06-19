package acp

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	cpesync "github.com/spachava753/cpe/internal/sync"
)

type closeTrackingRuntime struct {
	generate   func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error)
	closeCalls int
	closeErr   error
}

// Generate implements [runtime].
func (r *closeTrackingRuntime) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
	if r.generate == nil {
		return dialog, nil
	}
	return r.generate(ctx, dialog, opts)
}

// Close implements [runtime].
func (r *closeTrackingRuntime) Close() error {
	r.closeCalls++
	return r.closeErr
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestInit(t *testing.T) {
	fixture := setup(t, &noOpAcpClient{}, &config.RawConfig{}, unreachableRuntimeFactory)
	clientConn := fixture.ClientConn

	resp, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{
			Fs: &acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersion(1),
	})
	t.Log("called init")
	// we should not get an error on init connection
	be.Err(t, err, nil)
	// assert agent capabilities
	be.True(t, resp.AgentCapabilities.LoadSession)
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Close, &acp.SessionCloseCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Delete, &acp.SessionDeleteCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.List, &acp.SessionListCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Resume, &acp.SessionResumeCapabilities{})
	be.Equal(t, resp.AgentCapabilities.SessionCapabilities.Fork, &acp.SessionForkCapabilities{})
	be.True(t, resp.AgentCapabilities.PromptCapabilities.Audio)
	be.True(t, resp.AgentCapabilities.PromptCapabilities.Image)
	be.True(t, resp.AgentCapabilities.PromptCapabilities.EmbeddedContext)
}

func TestListSessions(t *testing.T) {
	fixture := setup(t, &noOpAcpClient{}, &config.RawConfig{}, unreachableRuntimeFactory)
	clientConn := fixture.ClientConn
	store := fixture.Store

	// seed the db
	sessionEntries := []storage.CreateACPSessionParams{
		{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionID: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "gpt-5.5",
			ThinkingLevel: "low",
		},
		{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir2",
				SessionID: "123abc",
			},
			LastMessageID: "",
			ModelRef:      "gpt-5.4-mini",
			ThinkingLevel: "xhigh",
		},
	}
	for _, se := range sessionEntries {
		be.Err(t, store.CreateACPSession(t.Context(), se), nil)
	}

	_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{
			Fs: &acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersion(1),
	})
	t.Log("called init")
	// we should not get an error on init connection
	be.Err(t, err, nil)

	// TODO: we should assert the order as well, as the order returned
	// will be based on most recent acp session first and descendng
	resp, err := clientConn.ListSessions(t.Context(), &acp.ListSessionsRequest{})
	be.Err(t, err, nil)
	be.Equal(t, len(resp.Sessions), len(sessionEntries))
}

func TestNewSession(t *testing.T) {
	fixture := setup(
		t,
		&noOpAcpClient{},
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
						ThinkingValues: []config.ThinkingValueConfig{
							{
								Name:        "Low",
								Value:       "low",
								Description: "Low thinking",
							},
							{
								Name:        "High",
								Value:       "high",
								Description: "High thinking",
							},
						},
					},
				},
			},
		},
		unreachableRuntimeFactory,
	)
	clientConn := fixture.ClientConn
	store := fixture.Store

	_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{
			Fs: &acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersion(1),
	})
	t.Log("called init")
	be.Err(t, err, nil)

	resp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
		Cwd:        "/rando/dir",
		McpServers: []acp.McpServer{},
	})
	be.Err(t, err, nil)
	be.True(t, resp.SessionID != "")
	be.Equal(t, len(*resp.ConfigOptions), 2)
	be.Equal(t, (*resp.ConfigOptions)[0].ID, modelRefConfigId)
	be.Equal(t, (*resp.ConfigOptions)[0].CurrentValue, any("test-model"))
	be.Equal(t, (*resp.ConfigOptions)[1].ID, thinkingLevelConfigId)
	be.Equal(t, (*resp.ConfigOptions)[1].CurrentValue, any("low"))

	storedSession, err := store.GetACPSession(t.Context(), resp.SessionID)
	be.Err(t, err, nil)
	be.Equal(t, storedSession.Session.Cwd, "/rando/dir")
	be.Equal(t, *storedSession.Session.Title, "untitled")
	be.Equal(t, storedSession.ModelRef, "test-model")
	be.Equal(t, storedSession.ThinkingLevel, "low")
	be.Equal(t, storedSession.LastMessageID, "")
}

func TestResumeSession(t *testing.T) {
	t.Run("existing session", func(t *testing.T) {
		var createdModelRefs []string
		fixture := setup(
			t,
			&noOpAcpClient{},
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
							ThinkingValues: []config.ThinkingValueConfig{
								{
									Name:        "Low",
									Value:       "low",
									Description: "Low thinking",
								},
							},
						},
					},
				},
			},
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				createdModelRefs = append(createdModelRefs, s.model)
				return testRuntime{}, nil
			},
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionID: "abc123",
				Title:     new("Existing session"),
			},
			LastMessageID: "",
			ModelRef:      "test-model",
			ThinkingLevel: "low",
		}), nil)

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		t.Log("called init")
		be.Err(t, err, nil)

		resp, err := clientConn.ResumeSession(t.Context(), &acp.ResumeSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
			SessionID:  "abc123",
		})
		be.Err(t, err, nil)
		be.Equal(t, len(*resp.ConfigOptions), 2)
		be.Equal(t, (*resp.ConfigOptions)[0].ID, modelRefConfigId)
		be.Equal(t, (*resp.ConfigOptions)[0].CurrentValue, any("test-model"))
		be.Equal(t, (*resp.ConfigOptions)[1].ID, thinkingLevelConfigId)
		be.Equal(t, (*resp.ConfigOptions)[1].CurrentValue, any("low"))
		be.Equal(t, createdModelRefs, []string{"test-model"})
	})

	t.Run("stale model ref", func(t *testing.T) {
		var createdModelRefs []string
		var createdSessions []session
		fixture := setup(
			t,
			&noOpAcpClient{},
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
							ThinkingValues: []config.ThinkingValueConfig{
								{
									Name:        "Low",
									Value:       "low",
									Description: "Low thinking",
								},
							},
						},
					},
				},
			},
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				createdModelRefs = append(createdModelRefs, s.model)
				createdSessions = append(createdSessions, s)
				return testRuntime{}, nil
			},
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionID: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "stale-model",
			ThinkingLevel: "stale-thinking",
		}), nil)

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		t.Log("called init")
		be.Err(t, err, nil)

		resp, err := clientConn.ResumeSession(t.Context(), &acp.ResumeSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
			SessionID:  "abc123",
		})
		be.Err(t, err, nil)
		be.Equal(t, len(*resp.ConfigOptions), 2)
		be.Equal(t, (*resp.ConfigOptions)[0].CurrentValue, any("test-model"))
		be.Equal(t, (*resp.ConfigOptions)[1].CurrentValue, any("low"))
		be.Equal(t, createdModelRefs, []string{"test-model"})
		if len(createdSessions) != 1 {
			t.Fatalf("runtime created with %d sessions, want 1", len(createdSessions))
		}
		createdSession := createdSessions[0]
		be.Equal(t, createdSession.id, acp.SessionId("abc123"))
		be.Equal(t, createdSession.cwd, "/rando/dir")
		be.Equal(t, createdSession.model, "test-model")
		be.Equal(t, createdSession.thinking, "low")

		storedSession, err := store.GetACPSession(t.Context(), "abc123")
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model")
		be.Equal(t, storedSession.ThinkingLevel, "low")
	})

	t.Run("stale thinking level", func(t *testing.T) {
		var createdModelRefs []string
		fixture := setup(
			t,
			&noOpAcpClient{},
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
							ThinkingValues: []config.ThinkingValueConfig{
								{
									Name:        "Low",
									Value:       "low",
									Description: "Low thinking",
								},
							},
						},
					},
				},
			},
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				createdModelRefs = append(createdModelRefs, s.model)
				return testRuntime{}, nil
			},
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionID: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "test-model",
			ThinkingLevel: "stale-thinking",
		}), nil)

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		t.Log("called init")
		be.Err(t, err, nil)

		resp, err := clientConn.ResumeSession(t.Context(), &acp.ResumeSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
			SessionID:  "abc123",
		})
		be.Err(t, err, nil)
		be.Equal(t, len(*resp.ConfigOptions), 2)
		be.Equal(t, (*resp.ConfigOptions)[0].CurrentValue, any("test-model"))
		be.Equal(t, (*resp.ConfigOptions)[1].CurrentValue, any("low"))
		be.Equal(t, createdModelRefs, []string{"test-model"})

		storedSession, err := store.GetACPSession(t.Context(), "abc123")
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model")
		be.Equal(t, storedSession.ThinkingLevel, "low")
	})
}

func TestLoadSession(t *testing.T) {
	var createdModelRefs []string
	var createdSessions []session
	testClient := &promptTestClient{}
	fixture := setup(
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
			},
		},
		func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
			createdModelRefs = append(createdModelRefs, s.model)
			createdSessions = append(createdSessions, s)
			return testRuntime{}, nil
		},
	)
	clientConn := fixture.ClientConn
	store := fixture.Store

	dialog := gai.Dialog{
		{
			Role:   gai.User,
			Blocks: []gai.Block{gai.TextBlock("hello")},
		},
		{
			Role:   gai.Assistant,
			Blocks: []gai.Block{gai.TextBlock("answer")},
		},
	}
	savedDialog := make(gai.Dialog, 0, len(dialog))
	for msg, err := range store.SaveDialog(t.Context(), slices.Values(dialog)) {
		be.Err(t, err, nil)
		savedDialog = append(savedDialog, msg)
	}
	lastMessageID := storage.GetMessageID(savedDialog[len(savedDialog)-1])
	be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acp.SessionInfo{
			Cwd:       "/rando/dir",
			SessionID: "abc123",
		},
		LastMessageID: lastMessageID,
		ModelRef:      "test-model",
		ThinkingLevel: "",
	}), nil)

	_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{
			Fs: &acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersion(1),
	})
	t.Log("called init")
	be.Err(t, err, nil)

	mcpServers := []acp.McpServer{
		acp.HttpMcpServer("parallel", "https://example.test/mcp", []acp.HttpHeader{}),
	}
	resp, err := clientConn.LoadSession(t.Context(), &acp.LoadSessionRequest{
		Cwd:        "/rando/dir",
		McpServers: mcpServers,
		SessionID:  "abc123",
	})
	be.Err(t, err, nil)
	be.Equal(t, len(*resp.ConfigOptions), 1)
	be.Equal(t, (*resp.ConfigOptions)[0].ID, modelRefConfigId)
	be.Equal(t, (*resp.ConfigOptions)[0].CurrentValue, any("test-model"))
	be.Equal(t, createdModelRefs, []string{"test-model"})
	if len(createdSessions) != 1 {
		t.Fatalf("runtime created with %d sessions, want 1", len(createdSessions))
	}
	createdSession := createdSessions[0]
	be.Equal(t, createdSession.id, acp.SessionId("abc123"))
	be.Equal(t, createdSession.cwd, "/rando/dir")
	be.Equal(t, createdSession.model, "test-model")
	be.Equal(t, createdSession.mcpServers, mcpServers)
	assertNotifications(t, testClient, []acp.SessionNotification{
		{
			SessionID: "abc123",
			Update:    expectedRPCUserMessageChunk("hello"),
		},
		{
			SessionID: "abc123",
			Update:    expectedRPCAgentMessageChunk("answer"),
		},
	})
}

func TestLoadSessionReplaysCompactionLineage(t *testing.T) {
	var createdModelRefs []string
	testClient := &promptTestClient{}
	fixture := setup(
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
			},
		},
		func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
			createdModelRefs = append(createdModelRefs, s.model)
			return testRuntime{}, nil
		},
	)
	clientConn := fixture.ClientConn
	store := fixture.Store

	prior := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original question")}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("original answer")}},
	}
	var savedPrior gai.Dialog
	for msg, err := range store.SaveDialog(t.Context(), slices.Values(prior)) {
		be.Err(t, err, nil)
		savedPrior = append(savedPrior, msg)
	}
	priorLeafID := storage.GetMessageID(savedPrior[len(savedPrior)-1])

	compactedRoot := gai.Message{
		Role:        gai.User,
		Blocks:      []gai.Block{gai.TextBlock("compacted summary")},
		ExtraFields: map[string]any{storage.MessageCompactionParentIDKey: priorLeafID},
	}
	compacted := gai.Dialog{
		compactedRoot,
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("answer after compaction")}},
	}
	var savedCompacted gai.Dialog
	for msg, err := range store.SaveDialog(t.Context(), slices.Values(compacted)) {
		be.Err(t, err, nil)
		savedCompacted = append(savedCompacted, msg)
	}
	lastMessageID := storage.GetMessageID(savedCompacted[len(savedCompacted)-1])

	be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
		Session: acp.SessionInfo{
			Cwd:       "/rando/dir",
			SessionID: "abc123",
		},
		LastMessageID: lastMessageID,
		ModelRef:      "test-model",
		ThinkingLevel: "",
	}), nil)

	_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{
			Fs: &acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersion(1),
	})
	t.Log("called init")
	be.Err(t, err, nil)

	_, err = clientConn.LoadSession(t.Context(), &acp.LoadSessionRequest{
		Cwd:        "/rando/dir",
		McpServers: []acp.McpServer{},
		SessionID:  "abc123",
	})
	be.Err(t, err, nil)
	be.Equal(t, createdModelRefs, []string{"test-model"})
	assertNotifications(t, testClient, []acp.SessionNotification{
		{
			SessionID: "abc123",
			Update:    expectedRPCUserMessageChunk("original question"),
		},
		{
			SessionID: "abc123",
			Update:    expectedRPCAgentMessageChunk("original answer"),
		},
		{
			SessionID: "abc123",
			Update:    expectedRPCUserMessageChunk("compacted summary"),
		},
		{
			SessionID: "abc123",
			Update:    expectedRPCAgentMessageChunk("answer after compaction"),
		},
	})
}

func TestCancel(t *testing.T) {
	t.Run("active prompt", func(t *testing.T) {
		generateStarted := make(chan struct{})
		var clientConn *acp.Client
		var store *storage.Sqlite
		rawCfg := config.RawConfig{
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
			},
		}
		fixture := setup(
			t,
			&noOpAcpClient{},
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{responses: []genFunc{
					func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
						close(generateStarted)
						<-ctx.Done()
						return gai.Response{}, ctx.Err()
					},
				}}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:           &gen,
					DialogSaver: store,
					CostAdder:   store,
					Cfg:         cfg,
					conn:        conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		t.Log("called init")
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		type promptResult struct {
			resp *acp.PromptResponse
			err  error
		}
		promptResultCh := make(chan promptResult, 1)
		go func() {
			resp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
				Prompt: []acp.ContentBlock{
					acp.TextContentBlock("Hello"),
				},
				SessionID: newSessionResp.SessionID,
			})
			promptResultCh <- promptResult{resp: resp, err: err}
		}()

		select {
		case <-generateStarted:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for runtime generation to start")
		}

		err = clientConn.Cancel(t.Context(), &acp.CancelNotification{
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)

		select {
		case result := <-promptResultCh:
			be.Err(t, result.err, nil)
			be.Equal(t, result.resp.StopReason, acp.StopReasonCancelled)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for prompt to return after cancellation")
		}

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		be.True(t, storedSession.LastMessageID != "")
	})

	t.Run("unknown session", func(t *testing.T) {
		agent := Agent{
			activeSessions: new(cpesync.Map[acp.SessionId, *cpesync.Guard[session]]),
		}

		err := agent.Cancel(t.Context(), &acp.CancelNotification{
			SessionID: "missing-session",
		})
		be.True(t, err != nil)
	})
}

func TestDeleteSession(t *testing.T) {
	var store *storage.Sqlite
	trackingRuntime := &closeTrackingRuntime{
		generate: func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
			generatedDialog := append(dialog, gai.Message{
				Role:   gai.Assistant,
				Blocks: []gai.Block{gai.TextBlock("assistant answer")},
			})
			savedDialog := make(gai.Dialog, 0, len(generatedDialog))
			for msg, err := range store.SaveDialog(ctx, slices.Values(generatedDialog)) {
				if err != nil {
					return nil, err
				}
				savedDialog = append(savedDialog, msg)
			}
			return savedDialog, nil
		},
	}
	var clientConn *acp.Client
	var rawDB *sql.DB
	fixture := setup(
		t,
		&noOpAcpClient{},
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
			},
		},
		func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
			return trackingRuntime, nil
		},
	)
	clientConn = fixture.ClientConn
	store = fixture.Store
	rawDB = fixture.RawDB

	_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{
			Fs: &acp.FileSystemCapabilities{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{
			Name:    "test-client",
			Title:   new("test client"),
			Version: "test",
		},
		ProtocolVersion: acp.ProtocolVersion(1),
	})
	t.Log("called init")
	be.Err(t, err, nil)

	newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
		Cwd:        "/rando/dir",
		McpServers: []acp.McpServer{},
	})
	be.Err(t, err, nil)
	be.True(t, newSessionResp.SessionID != "")

	promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
		Prompt: []acp.ContentBlock{
			acp.TextContentBlock("Hello"),
		},
		SessionID: newSessionResp.SessionID,
	})
	be.Err(t, err, nil)
	be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)

	storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
	be.Err(t, err, nil)
	be.True(t, storedSession.LastMessageID != "")
	storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
	be.Err(t, err, nil)
	be.Equal(t, len(storedDialog), 2)
	be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM acp_sessions WHERE id = ?", newSessionResp.SessionID), 1)
	be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 2)
	be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM blocks"), 2)

	_, err = clientConn.DeleteSession(t.Context(), &acp.DeleteSessionRequest{
		SessionID: newSessionResp.SessionID,
	})
	be.Err(t, err, nil)
	be.Equal(t, trackingRuntime.closeCalls, 1)

	listResp, err := clientConn.ListSessions(t.Context(), &acp.ListSessionsRequest{})
	be.Err(t, err, nil)
	be.True(t, !slices.ContainsFunc(listResp.Sessions, func(si acp.SessionInfo) bool {
		return si.SessionID == newSessionResp.SessionID
	}))

	_, err = store.GetACPSession(t.Context(), newSessionResp.SessionID)
	be.True(t, err != nil)

	_, err = clientConn.CloseSession(t.Context(), &acp.CloseSessionRequest{
		SessionID: newSessionResp.SessionID,
	})
	be.True(t, err != nil)
	be.Equal(t, trackingRuntime.closeCalls, 1)
	be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM acp_sessions WHERE id = ?", newSessionResp.SessionID), 0)
	be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 0)
	be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM blocks"), 0)
}

func TestForkSession(t *testing.T) {
	forkTestConfig := func() *config.RawConfig {
		return &config.RawConfig{
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
			},
		}
	}

	t.Run("fork shares history and diverges", func(t *testing.T) {
		var store *storage.Sqlite
		savingRuntime := &closeTrackingRuntime{
			generate: func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
				generatedDialog := append(dialog, gai.Message{
					Role:   gai.Assistant,
					Blocks: []gai.Block{gai.TextBlock("assistant answer")},
				})
				savedDialog := make(gai.Dialog, 0, len(generatedDialog))
				for msg, err := range store.SaveDialog(ctx, slices.Values(generatedDialog)) {
					if err != nil {
						return nil, err
					}
					savedDialog = append(savedDialog, msg)
				}
				return savedDialog, nil
			},
		}
		fixture := setup(
			t,
			&noOpAcpClient{},
			forkTestConfig(),
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				return savingRuntime, nil
			},
		)
		clientConn := fixture.ClientConn
		store = fixture.Store
		rawDB := fixture.RawDB

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("Hello"),
			},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 2)

		forkResp, err := clientConn.ForkSession(t.Context(), &acp.ForkSessionRequest{
			Cwd:       "/rando/dir",
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.True(t, forkResp.SessionID != "")
		be.True(t, forkResp.SessionID != newSessionResp.SessionID)

		// fork response surfaces the model config option with the inherited value
		be.Equal(t, len(*forkResp.ConfigOptions), 1)
		be.True(t, (*forkResp.ConfigOptions)[0].Type == acp.SessionConfigOptionTypeSelect)
		be.Equal(t, (*forkResp.ConfigOptions)[0].CurrentValue, any("test-model"))

		// the fork shares the source's message chain without copying rows
		srcSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		forkSession, err := store.GetACPSession(t.Context(), forkResp.SessionID)
		be.Err(t, err, nil)
		be.Equal(t, forkSession.LastMessageID, srcSession.LastMessageID)
		be.Equal(t, forkSession.ModelRef, "test-model")
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 2)

		listResp, err := clientConn.ListSessions(t.Context(), &acp.ListSessionsRequest{})
		be.Err(t, err, nil)
		be.True(t, slices.ContainsFunc(listResp.Sessions, func(si acp.SessionInfo) bool {
			return si.SessionID == forkResp.SessionID
		}))

		// prompting the fork branches off the shared chain
		promptResp, err = clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("Summarize the conversation"),
			},
			SessionID: forkResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)

		forkSession, err = store.GetACPSession(t.Context(), forkResp.SessionID)
		be.Err(t, err, nil)
		forkDialog, err := storage.GetDialogForMessage(t.Context(), store, forkSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(forkDialog), 4)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 4)

		// the source session's history is unaffected by the fork's prompt
		unchangedSrcSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		be.Equal(t, unchangedSrcSession.LastMessageID, srcSession.LastMessageID)
	})

	t.Run("delete source preserves fork history", func(t *testing.T) {
		var store *storage.Sqlite
		savingRuntime := &closeTrackingRuntime{
			generate: func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
				generatedDialog := append(dialog, gai.Message{
					Role:   gai.Assistant,
					Blocks: []gai.Block{gai.TextBlock("assistant answer")},
				})
				savedDialog := make(gai.Dialog, 0, len(generatedDialog))
				for msg, err := range store.SaveDialog(ctx, slices.Values(generatedDialog)) {
					if err != nil {
						return nil, err
					}
					savedDialog = append(savedDialog, msg)
				}
				return savedDialog, nil
			},
		}
		fixture := setup(
			t,
			&noOpAcpClient{},
			forkTestConfig(),
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				return savingRuntime, nil
			},
		)
		clientConn := fixture.ClientConn
		store = fixture.Store
		rawDB := fixture.RawDB

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("Hello"),
			},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)

		forkResp, err := clientConn.ForkSession(t.Context(), &acp.ForkSessionRequest{
			Cwd:       "/rando/dir",
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)

		// diverge the fork so it owns a branch of its own
		_, err = clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("Summarize the conversation"),
			},
			SessionID: forkResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 4)

		// deleting the source only removes the source session row; the shared
		// history is still reachable from the fork
		_, err = clientConn.DeleteSession(t.Context(), &acp.DeleteSessionRequest{
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM acp_sessions"), 1)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 4)

		forkSession, err := store.GetACPSession(t.Context(), forkResp.SessionID)
		be.Err(t, err, nil)
		forkDialog, err := storage.GetDialogForMessage(t.Context(), store, forkSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(forkDialog), 4)

		// deleting the fork removes the remaining chain
		_, err = clientConn.DeleteSession(t.Context(), &acp.DeleteSessionRequest{
			SessionID: forkResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM acp_sessions"), 0)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM messages"), 0)
		be.Equal(t, countRows(t, rawDB, "SELECT COUNT(*) FROM blocks"), 0)
	})

	t.Run("unknown session", func(t *testing.T) {
		fixture := setup(t, &noOpAcpClient{}, forkTestConfig(), unreachableRuntimeFactory)
		clientConn := fixture.ClientConn

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)

		_, err = clientConn.ForkSession(t.Context(), &acp.ForkSessionRequest{
			Cwd:       "/rando/dir",
			SessionID: "does-not-exist",
		})
		be.True(t, err != nil)
	})
}

func TestCloseSession(t *testing.T) {
	t.Run("active session", func(t *testing.T) {
		trackingRuntime := &closeTrackingRuntime{}
		fixture := setup(
			t,
			&noOpAcpClient{},
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
				},
			},
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				return trackingRuntime, nil
			},
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionID: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "test-model",
			ThinkingLevel: "",
		}), nil)

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		t.Log("called init")
		be.Err(t, err, nil)

		_, err = clientConn.ResumeSession(t.Context(), &acp.ResumeSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
			SessionID:  "abc123",
		})
		be.Err(t, err, nil)

		_, err = clientConn.CloseSession(t.Context(), &acp.CloseSessionRequest{
			SessionID: "abc123",
		})
		be.Err(t, err, nil)
		be.Equal(t, trackingRuntime.closeCalls, 1)

		_, err = clientConn.CloseSession(t.Context(), &acp.CloseSessionRequest{
			SessionID: "abc123",
		})
		be.True(t, err != nil)
		be.Equal(t, trackingRuntime.closeCalls, 1)
	})

	t.Run("runtime close error", func(t *testing.T) {
		trackingRuntime := &closeTrackingRuntime{closeErr: errors.New("close failed")}
		fixture := setup(
			t,
			&noOpAcpClient{},
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
				},
			},
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				return trackingRuntime, nil
			},
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				Cwd:       "/rando/dir",
				SessionID: "abc123",
			},
			LastMessageID: "",
			ModelRef:      "test-model",
			ThinkingLevel: "",
		}), nil)

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
				Fs: &acp.FileSystemCapabilities{
					ReadTextFile:  false,
					WriteTextFile: false,
				},
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersion(1),
		})
		t.Log("called init")
		be.Err(t, err, nil)

		_, err = clientConn.ResumeSession(t.Context(), &acp.ResumeSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
			SessionID:  "abc123",
		})
		be.Err(t, err, nil)

		_, err = clientConn.CloseSession(t.Context(), &acp.CloseSessionRequest{
			SessionID: "abc123",
		})
		be.True(t, err != nil)
		be.Equal(t, trackingRuntime.closeCalls, 1)
	})
}
