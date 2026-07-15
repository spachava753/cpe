package acp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	cpesync "github.com/spachava753/cpe/internal/sync"
)

type promptTestClient struct {
	mu                    sync.Mutex
	capturedNotifications []acp.SessionNotification
}

// Update implements [acp.SessionClientHandler].
func (t *promptTestClient) Update(ctx context.Context, params *acp.SessionNotification) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capturedNotifications = append(t.capturedNotifications, *params)
	return nil
}

func (t *promptTestClient) notifications() []acp.SessionNotification {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.capturedNotifications)
}

func (t *promptTestClient) waitForNotifications(tb *testing.T, count int) []acp.SessionNotification {
	tb.Helper()
	deadline := time.After(5 * time.Second)
	for range time.Tick(10 * time.Millisecond) {
		notifications := t.notifications()
		if len(notifications) >= count {
			return notifications
		}

		select {
		case <-tb.Context().Done():
			tb.Fatalf("context cancelled while waiting for %d notifications", count)
		case <-deadline:
			tb.Fatalf("timed out waiting for %d notifications; got %d: %#v", count, len(notifications), notifications)
		default:
		}
	}
	panic("unreachable")
}

func assertNotifications(tb *testing.T, client *promptTestClient, want []acp.SessionNotification) {
	tb.Helper()
	be.Equal(tb, sortedNotifications(client.waitForNotifications(tb, len(want))), sortedNotifications(want))
}

func sortedNotifications(notifications []acp.SessionNotification) []acp.SessionNotification {
	sorted := slices.Clone(notifications)
	slices.SortFunc(sorted, func(a, b acp.SessionNotification) int {
		return strings.Compare(notificationSortKey(a), notificationSortKey(b))
	})
	return sorted
}

func notificationSortKey(notification acp.SessionNotification) string {
	key, err := json.Marshal(notification)
	if err != nil {
		panic(err)
	}
	return string(key)
}

type genFunc func(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error)

// testGen represents a test time generator. [testGen.responses] is a list of functions
// that will be called in order they are defined
type testGen struct {
	responses []genFunc
	called    int
}

func (g *testGen) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	gen := g.responses[g.called]
	g.called++
	return gen(ctx, dialog, opts)
}

func (g *testGen) Register(tool gai.Tool) error {
	return nil
}

var _ gai.ToolCallingGenerator = (*testGen)(nil)

// testRuntime is a test time implementation of a [runtime]
type testRuntime struct {
	*Loop
}

func (r testRuntime) Close() error {
	return nil
}

func TestSkillSlashCommands(t *testing.T) {
	var (
		clientConn              *acp.Client
		store                   *storage.Sqlite
		cwd                     = t.TempDir()
		testClient              = &promptTestClient{}
		sessionID               acp.SessionId
		commandNotification     acp.SessionNotification
		commandNotificationSeen int
		rawCfg                  = config.RawConfig{
			Models: []config.ModelConfig{{
				Model: config.Model{
					Ref:           "test-model",
					DisplayName:   "Test Model",
					ID:            "test-model",
					Type:          "responses",
					ContextWindow: 100,
				},
			}},
		}
	)
	createACPSkill(t, filepath.Join(cwd, ".agents", "skills"), "domain-modeling", map[string]any{
		"name":        "domain-modeling",
		"description": "Domain modeling help",
		"group":       "design",
	})
	createACPSkill(t, filepath.Join(cwd, ".agents", "skills"), "hidden-skill", map[string]any{
		"name":                     "hidden-skill",
		"description":              "Hidden help",
		"disable-model-invocation": true,
	})

	fixture := setup(
		t,
		testClient,
		&rawCfg,
		func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
			gen := testGen{responses: []genFunc{
				func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
					be.Equal(t, countNotifications(testClient.notifications(), commandNotification), 3)
					be.Equal(t, len(d), 1)
					be.Equal(t, d[0].Role, gai.User)
					be.Equal(t, d[0].Blocks[0].Content.String(), "Use ./.agents/skills/domain-modeling and /skill:missing")
					return gai.Response{
						Candidates: []gai.Message{{
							Role:   gai.Assistant,
							Blocks: []gai.Block{gai.TextBlock("done")},
						}},
						FinishReason: gai.EndTurn,
					}, nil
				},
				func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
					commandNotificationSeen = countNotifications(testClient.notifications(), commandNotification)
					be.Equal(t, commandNotificationSeen, 4)
					be.Equal(t, len(d), 3)
					be.Equal(t, d[2].Role, gai.User)
					be.Equal(t, d[2].Blocks[0].Content.String(), "Again ./.agents/skills/domain-modeling")
					return gai.Response{
						Candidates: []gai.Message{{
							Role:   gai.Assistant,
							Blocks: []gai.Block{gai.TextBlock("done again")},
						}},
						FinishReason: gai.EndTurn,
					}, nil
				},
			}}
			cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
			be.Err(t, err, nil)
			return testRuntime{Loop: &Loop{
				G:     &gen,
				Store: store,
				Cfg:   cfg,
				conn:  conn,
			}}, nil
		},
	)
	clientConn = fixture.ClientConn
	store = fixture.Store

	_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
		ClientCapabilities: &acp.ClientCapabilities{Terminal: true},
		ProtocolVersion:    acp.ProtocolVersion(1),
	})
	be.Err(t, err, nil)

	newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	be.Err(t, err, nil)
	sessionID = newSessionResp.SessionID
	be.Equal(t, len(testClient.notifications()), 0)

	inputDomain := acp.UnstructuredAvailableCommandInput("Domain modeling help")
	inputHidden := acp.UnstructuredAvailableCommandInput("Hidden help")
	commandNotification = acp.SessionNotification{
		SessionID: sessionID,
		Update: acp.AvailableCommandsUpdateSessionUpdate([]acp.AvailableCommand{
			{Name: "skill:domain-modeling", Description: "Domain modeling help", Input: &inputDomain},
			{Name: "skill:hidden-skill", Description: "Hidden help", Input: &inputHidden},
		}),
	}

	_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
		ConfigID:  modelRefConfigId,
		SessionID: sessionID,
		Value:     "test-model",
	})
	be.Err(t, err, nil)
	be.Equal(t, countNotifications(testClient.notifications(), commandNotification), 1)
	_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
		ConfigID:  modelRefConfigId,
		SessionID: sessionID,
		Value:     "test-model",
	})
	be.Err(t, err, nil)
	be.Equal(t, countNotifications(testClient.notifications(), commandNotification), 2)

	promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
		Prompt:    []acp.ContentBlock{acp.TextContentBlock("Use /skill:domain-modeling and /skill:missing")},
		SessionID: sessionID,
	})
	be.Err(t, err, nil)
	be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
	be.Equal(t, countNotifications(testClient.notifications(), commandNotification), 3)

	promptResp, err = clientConn.Prompt(t.Context(), &acp.PromptRequest{
		Prompt:    []acp.ContentBlock{acp.TextContentBlock("Again /skill:domain-modeling")},
		SessionID: sessionID,
	})
	be.Err(t, err, nil)
	be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
	be.Equal(t, commandNotificationSeen, 4)
	be.Equal(t, countNotifications(testClient.notifications(), commandNotification), 4)

	storedSession, err := store.GetACPSession(t.Context(), sessionID)
	be.Err(t, err, nil)
	storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
	be.Err(t, err, nil)
	be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Use ./.agents/skills/domain-modeling and /skill:missing")
	be.Equal(t, storedDialog[2].Blocks[0].Content.String(), "Again ./.agents/skills/domain-modeling")
}

func countNotifications(notifications []acp.SessionNotification, want acp.SessionNotification) int {
	count := 0
	for _, notification := range notifications {
		if reflect.DeepEqual(notification, want) {
			count++
		}
	}
	return count
}

func TestSkillSlashCommandsLifecycleMethodsDoNotPublish(t *testing.T) {
	sourceCwd := t.TempDir()
	forkCwd := t.TempDir()
	loadCwd := t.TempDir()
	resumeCwd := t.TempDir()
	for _, cwd := range []string{sourceCwd, forkCwd, loadCwd, resumeCwd} {
		createACPSkill(t, filepath.Join(cwd, ".agents", "skills"), "codebase-design", map[string]any{
			"name":        "codebase-design",
			"description": "Design help",
		})
	}

	t.Run("new", func(t *testing.T) {
		client := &promptTestClient{}
		fixture := setup(t, client, &config.RawConfig{}, unreachableRuntimeFactory)
		clientConn := fixture.ClientConn
		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{Terminal: true},
			ProtocolVersion:    acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)
		_, err = clientConn.NewSession(t.Context(), &acp.NewSessionRequest{Cwd: sourceCwd})
		be.Err(t, err, nil)
		be.Equal(t, len(client.notifications()), 0)
	})

	t.Run("fork", func(t *testing.T) {
		client := &promptTestClient{}
		fixture := setup(t, client, &config.RawConfig{}, unreachableRuntimeFactory)
		clientConn := fixture.ClientConn
		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{Terminal: true},
			ProtocolVersion:    acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)
		sourceResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{Cwd: sourceCwd})
		be.Err(t, err, nil)
		_, err = clientConn.ForkSession(t.Context(), &acp.ForkSessionRequest{
			Cwd:       forkCwd,
			SessionID: sourceResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, len(client.notifications()), 0)
	})

	t.Run("load", func(t *testing.T) {
		client := &promptTestClient{}
		fixture := setup(t, client, &config.RawConfig{}, unreachableRuntimeFactory)
		clientConn := fixture.ClientConn
		store := fixture.Store
		var lastMessageID string
		for msg, err := range store.SaveDialog(t.Context(), slices.Values(gai.Dialog{{
			Role:   gai.User,
			Blocks: []gai.Block{gai.TextBlock("hello")},
		}})) {
			be.Err(t, err, nil)
			lastMessageID = storage.GetMessageID(msg)
		}
		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session:       acp.SessionInfo{Cwd: loadCwd, SessionID: "load-session"},
			LastMessageID: lastMessageID,
		}), nil)
		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{Terminal: true},
			ProtocolVersion:    acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)
		_, err = clientConn.LoadSession(t.Context(), &acp.LoadSessionRequest{Cwd: loadCwd, SessionID: "load-session"})
		be.Err(t, err, nil)
		input := acp.UnstructuredAvailableCommandInput("Design help")
		be.Equal(t, countNotifications(client.notifications(), acp.SessionNotification{
			SessionID: "load-session",
			Update: acp.AvailableCommandsUpdateSessionUpdate([]acp.AvailableCommand{
				{Name: "skill:codebase-design", Description: "Design help", Input: &input},
			}),
		}), 0)
	})

	t.Run("resume", func(t *testing.T) {
		client := &promptTestClient{}
		fixture := setup(t, client, &config.RawConfig{}, unreachableRuntimeFactory)
		clientConn := fixture.ClientConn
		store := fixture.Store
		be.Err(t, store.CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{Cwd: resumeCwd, SessionID: "resume-session"},
		}), nil)
		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{Terminal: true},
			ProtocolVersion:    acp.ProtocolVersion(1),
		})
		be.Err(t, err, nil)
		_, err = clientConn.ResumeSession(t.Context(), &acp.ResumeSessionRequest{Cwd: resumeCwd, SessionID: "resume-session"})
		be.Err(t, err, nil)
		be.Equal(t, len(client.notifications()), 0)
	})
}

func TestPrompt(t *testing.T) {
	t.Run("rejects prompt before model selection", func(t *testing.T) {
		fixture := setup(
			t,
			&noOpAcpClient{},
			&config.RawConfig{
				Models: []config.ModelConfig{{
					Model: config.Model{
						Ref:         "test-model",
						DisplayName: "Test Model",
						ID:          "test-model",
						Type:        "responses",
					},
				}},
			},
			unreachableRuntimeFactory,
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        t.TempDir(),
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
			SessionID: newSessionResp.SessionID,
		})
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), "cannot prompt before selecting a model"))

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "")
		be.Equal(t, storedSession.ThinkingLevel, "")
		be.Equal(t, storedSession.LastMessageID, "")
	})

	t.Run("happy path", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			cwd        = t.TempDir()
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
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
			}
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{
					responses: []genFunc{
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							return gai.Response{
								Candidates: []gai.Message{{
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
								}},
								FinishReason: gai.EndTurn,
								UsageMetadata: gai.Metadata{
									gai.UsageMetricInputTokens:      80,
									gai.UsageMetricGenerationTokens: 10,
								},
							}, nil
						},
					},
				}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{
					ModelRef: s.model,
					// TODO: Should we set timeout?
					// TODO: Should we set gen opts?
				})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
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
		be.Err(t, err, nil) // we should not get an error on init connection

		// new session
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)
		sessionId := newSessionResp.SessionID
		be.True(t, sessionId != "") // session id cannot be empty
		modelOptions := acp.UngroupedSessionConfigSelectOptions{
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
		}
		expectedConfigOptions := []acp.SessionConfigOption{
			acp.SelectSessionConfigOption(modelRefConfigId, "Model", acp.SessionConfigValueId(""), acp.SessionConfigSelectOptions{Ungrouped: &modelOptions}),
		}
		expectedConfigOptions[0].Category = new(acp.SessionConfigOptionCategoryModel)
		expectedConfigOptions[0].CurrentValue = ""
		expectedConfigOptions[0].Description = new("Choose model")
		be.Equal(t, *newSessionResp.ConfigOptions, expectedConfigOptions)
		// set config option
		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: sessionId,
			Value:     "test-model",
		})
		be.Err(t, err, nil)
		// prompt
		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("Hello"),
			},
			SessionID: sessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 90, InputTokens: 80, OutputTokens: 10})
		assertNotifications(t, testClient, []acp.SessionNotification{
			{
				SessionID: sessionId,
				Update:    expectedRPCAgentThoughtChunk("let me think"),
			},
			{
				SessionID: sessionId,
				Update:    expectedRPCAgentMessageChunk("here is the answer:"),
			},
			{
				SessionID: sessionId,
				Update: expectedUsageUpdate(90, 100, &acp.Cost{
					Amount:   0.00009,
					Currency: "USD",
				}),
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), sessionId)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		// This fixture returns a single assistant message, so the final stored message
		// is the only assistant message whose usage metadata we can assert here. A
		// real prompt turn can persist several generated messages, including
		// assistant tool-call messages and tool-result messages; the production
		// expectation is that every generated assistant message has its own usage
		// metadata attached and stored.
		storedAssistant := storedDialog[len(storedDialog)-1]
		inputTokens, ok := storedAssistant.ExtraFields[storage.AgentMetadataInputTokensKey].(int64)
		be.True(t, ok)
		be.Equal(t, inputTokens, int64(80))
		outputTokens, ok := storedAssistant.ExtraFields[storage.AgentMetadataOutputTokensKey].(int64)
		be.True(t, ok)
		be.Equal(t, outputTokens, int64(10))
	})
	t.Run("compaction", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			gen        *testGen
			cwd        = t.TempDir()
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
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
						Compaction: &config.RawCompactionConfig{
							AutoTriggerThreshold:      0.8,
							MaxAutoCompactionRestarts: 5,
							ToolDescription:           "compaction",
							InputSchema:               jsonschema.Schema{},
							InitialMessageTemplate:    `compacted conversation: {{ index .ToolArguments "summary" }}`,
						},
					},
				},
			}
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen = &testGen{
					responses: []genFunc{
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							be.Equal(t, len(d), 1)
							be.Equal(t, d[0].Role, gai.User)
							be.Equal(t, d[0].Blocks[0].Content.String(), "Hello")

							compactionBlock, err := gai.ToolCallBlock("compact-call-1", config.CompactionToolName, map[string]any{
								"summary": "conversation compacted state",
							})
							if err != nil {
								return gai.Response{}, err
							}
							return gai.Response{
								Candidates: []gai.Message{{
									Role:            gai.Assistant,
									Blocks:          []gai.Block{compactionBlock},
									ToolResultError: false,
									ExtraFields:     map[string]any{},
								}},
								FinishReason: gai.ToolUse,
								UsageMetadata: gai.Metadata{
									gai.UsageMetricInputTokens:      80,
									gai.UsageMetricGenerationTokens: 10,
								},
							}, nil
						},
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							be.Equal(t, len(d), 1)
							be.Equal(t, d[0].Role, gai.User)
							be.Equal(t, d[0].Blocks[0].Content.String(), "compacted conversation: conversation compacted state")

							return gai.Response{
								Candidates: []gai.Message{{
									Role: gai.Assistant,
									Blocks: []gai.Block{
										gai.TextBlock("continued after compaction"),
									},
									ToolResultError: false,
									ExtraFields:     map[string]any{},
								}},
								FinishReason: gai.EndTurn,
								UsageMetadata: gai.Metadata{
									gai.UsageMetricInputTokens:      5,
									gai.UsageMetricGenerationTokens: 2,
								},
							}, nil
						},
					},
				}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{
					ModelRef: s.model,
					// TODO: Should we set timeout?
					// TODO: Should we set gen opts?
				})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
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
		be.Err(t, err, nil) // we should not get an error on init connection

		// new session
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)
		sessionId := newSessionResp.SessionID
		be.True(t, sessionId != "") // session id cannot be empty
		modelOptions := acp.UngroupedSessionConfigSelectOptions{
			{
				Description: new(`Type: responses
Base Url: https://customurl.com/v1
Context Window: 100
Input Cost: 1.00
Output Cost: 1.00`),
				Name:  "Test Model",
				Value: "test-model",
			},
		}
		expectedConfigOptions := []acp.SessionConfigOption{
			acp.SelectSessionConfigOption(modelRefConfigId, "Model", acp.SessionConfigValueId(""), acp.SessionConfigSelectOptions{Ungrouped: &modelOptions}),
		}
		expectedConfigOptions[0].Category = new(acp.SessionConfigOptionCategoryModel)
		expectedConfigOptions[0].CurrentValue = ""
		expectedConfigOptions[0].Description = new("Choose model")
		be.Equal(t, *newSessionResp.ConfigOptions, expectedConfigOptions)
		// set config option
		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: sessionId,
			Value:     "test-model",
		})
		be.Err(t, err, nil)
		// prompt
		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("Hello"),
			},
			SessionID: sessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 7, InputTokens: 5, OutputTokens: 2})
		be.True(t, gen != nil)
		be.Equal(t, gen.called, 2)
		notifications := testClient.waitForNotifications(t, 5)
		be.Equal(t, sortedNotifications(notifications), sortedNotifications([]acp.SessionNotification{
			{
				SessionID: sessionId,
				Update: expectedPendingToolCallUpdate("compact-call-1", config.CompactionToolName, map[string]any{
					"summary": "conversation compacted state",
				}),
			},
			{
				SessionID: sessionId,
				Update: expectedUsageUpdate(90, 100, &acp.Cost{
					Amount:   0.00009,
					Currency: "USD",
				}),
			},
			{
				SessionID: sessionId,
				Update:    expectedRPCUserMessageChunk("compacted conversation: conversation compacted state"),
			},
			{
				SessionID: sessionId,
				Update:    expectedRPCAgentMessageChunk("continued after compaction"),
			},
			{
				SessionID: sessionId,
				Update: expectedUsageUpdate(7, 100, &acp.Cost{
					Amount:   0.000097,
					Currency: "USD",
				}),
			},
		}))
		var usageUpdateUsed []uint64
		for _, notification := range notifications {
			if notification.Update.SessionUpdate == acp.SessionUpdateTypeUsageUpdate {
				usageUpdateUsed = append(usageUpdateUsed, notification.Update.Used)
			}
		}
		slices.Sort(usageUpdateUsed)
		be.Equal(t, usageUpdateUsed, []uint64{7, 90})

		storedSession, err := store.GetACPSession(t.Context(), sessionId)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 2)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "compacted conversation: conversation compacted state")
		compactionParentID, ok := storedDialog[0].ExtraFields[storage.MessageCompactionParentIDKey].(string)
		be.True(t, ok)
		be.True(t, compactionParentID != "")

		parentMessages, err := store.GetMessages(t.Context(), []string{compactionParentID})
		be.Err(t, err, nil)
		var compactionParent gai.Message
		var foundCompactionParent bool
		for msg := range parentMessages {
			compactionParent = msg
			foundCompactionParent = true
		}
		be.True(t, foundCompactionParent)
		be.Equal(t, compactionParent.Role, gai.Assistant)
		be.Equal(t, compactionParent.Blocks[0].BlockType, gai.ToolCall)
		be.Equal(t, compactionParent.Blocks[0].ID, "compact-call-1")

		storedAssistant := storedDialog[len(storedDialog)-1]
		be.Equal(t, storedAssistant.Blocks[0].Content.String(), "continued after compaction")
		inputTokens, ok := storedAssistant.ExtraFields[storage.AgentMetadataInputTokensKey].(int64)
		be.True(t, ok)
		be.Equal(t, inputTokens, int64(5))
		outputTokens, ok := storedAssistant.ExtraFields[storage.AgentMetadataOutputTokensKey].(int64)
		be.True(t, ok)
		be.Equal(t, outputTokens, int64(2))
	})
	t.Run("continues existing session history", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			cwd        = t.TempDir()
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
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
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{
					responses: []genFunc{
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							be.Equal(t, len(d), 3)
							be.Equal(t, d[0].Role, gai.User)
							be.Equal(t, d[0].Blocks[0].Content.String(), "seed user")
							be.Equal(t, d[1].Role, gai.Assistant)
							be.Equal(t, d[1].Blocks[0].Content.String(), "seed assistant")
							be.Equal(t, d[2].Role, gai.User)
							be.Equal(t, d[2].Blocks[0].Content.String(), "follow-up")

							return gai.Response{
								Candidates: []gai.Message{{
									Role:            gai.Assistant,
									Blocks:          []gai.Block{gai.TextBlock("continued answer")},
									ToolResultError: false,
									ExtraFields:     map[string]any{},
								}},
								FinishReason: gai.EndTurn,
								UsageMetadata: gai.Metadata{
									gai.UsageMetricInputTokens:      12,
									gai.UsageMetricGenerationTokens: 4,
								},
							}, nil
						},
					},
				}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		seed := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("seed user")}},
			{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("seed assistant")}},
		}
		var lastMessageID string
		for msg, err := range store.SaveDialog(t.Context(), slices.Values(seed)) {
			be.Err(t, err, nil)
			lastMessageID = storage.GetMessageID(msg)
		}
		_, err = store.AddACPSessionMessage(t.Context(), newSessionResp.SessionID, "", lastMessageID)
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("follow-up")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 16, InputTokens: 12, OutputTokens: 4})
		assertNotifications(t, testClient, []acp.SessionNotification{
			{
				SessionID: newSessionResp.SessionID,
				Update:    expectedRPCAgentMessageChunk("continued answer"),
			},
			{
				SessionID: newSessionResp.SessionID,
				Update: expectedUsageUpdate(16, 100, &acp.Cost{
					Amount:   0.000016,
					Currency: "USD",
				}),
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model")
		be.Equal(t, storedSession.ThinkingLevel, "")
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 4)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "seed user")
		be.Equal(t, storedDialog[1].Blocks[0].Content.String(), "seed assistant")
		be.Equal(t, storedDialog[2].Blocks[0].Content.String(), "follow-up")
		be.Equal(t, storedDialog[3].Blocks[0].Content.String(), "continued answer")
	})
	t.Run("reuses runtime and accumulates history across prompts", func(t *testing.T) {
		var (
			clientConn  *acp.Client
			store       *storage.Sqlite
			gen         *testGen
			factoryCall int
			cwd         = t.TempDir()
			testClient  = &promptTestClient{}
			rawCfg      = config.RawConfig{
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
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				factoryCall++
				gen = &testGen{
					responses: []genFunc{
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							be.Equal(t, len(d), 1)
							be.Equal(t, d[0].Role, gai.User)
							be.Equal(t, d[0].Blocks[0].Content.String(), "first prompt")

							return gai.Response{
								Candidates: []gai.Message{{
									Role:            gai.Assistant,
									Blocks:          []gai.Block{gai.TextBlock("first answer")},
									ToolResultError: false,
									ExtraFields:     map[string]any{},
								}},
								FinishReason: gai.EndTurn,
								UsageMetadata: gai.Metadata{
									gai.UsageMetricInputTokens:      5,
									gai.UsageMetricGenerationTokens: 2,
								},
							}, nil
						},
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							be.Equal(t, len(d), 3)
							be.Equal(t, d[0].Role, gai.User)
							be.Equal(t, d[0].Blocks[0].Content.String(), "first prompt")
							be.Equal(t, d[1].Role, gai.Assistant)
							be.Equal(t, d[1].Blocks[0].Content.String(), "first answer")
							be.Equal(t, d[2].Role, gai.User)
							be.Equal(t, d[2].Blocks[0].Content.String(), "second prompt")

							return gai.Response{
								Candidates: []gai.Message{{
									Role:            gai.Assistant,
									Blocks:          []gai.Block{gai.TextBlock("second answer")},
									ToolResultError: false,
									ExtraFields:     map[string]any{},
								}},
								FinishReason: gai.EndTurn,
								UsageMetadata: gai.Metadata{
									gai.UsageMetricInputTokens:      6,
									gai.UsageMetricGenerationTokens: 3,
								},
							}, nil
						},
					},
				}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		firstResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("first prompt")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, firstResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, firstResp.Usage, &acp.Usage{TotalTokens: 7, InputTokens: 5, OutputTokens: 2})

		secondResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("second prompt")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, secondResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, secondResp.Usage, &acp.Usage{TotalTokens: 9, InputTokens: 6, OutputTokens: 3})
		be.Equal(t, factoryCall, 1)
		be.True(t, gen != nil)
		be.Equal(t, gen.called, 2)
		firstCost := float64(5)/1_000_000 + float64(2)/1_000_000
		secondCost := firstCost + float64(6)/1_000_000 + float64(3)/1_000_000
		assertNotifications(t, testClient, []acp.SessionNotification{
			{
				SessionID: newSessionResp.SessionID,
				Update:    expectedRPCAgentMessageChunk("first answer"),
			},
			{
				SessionID: newSessionResp.SessionID,
				Update: expectedUsageUpdate(7, 100, &acp.Cost{
					Amount:   firstCost,
					Currency: "USD",
				}),
			},
			{
				SessionID: newSessionResp.SessionID,
				Update:    expectedRPCAgentMessageChunk("second answer"),
			},
			{
				SessionID: newSessionResp.SessionID,
				Update: expectedUsageUpdate(9, 100, &acp.Cost{
					Amount:   secondCost,
					Currency: "USD",
				}),
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 4)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "first prompt")
		be.Equal(t, storedDialog[1].Blocks[0].Content.String(), "first answer")
		be.Equal(t, storedDialog[2].Blocks[0].Content.String(), "second prompt")
		be.Equal(t, storedDialog[3].Blocks[0].Content.String(), "second answer")
	})
	t.Run("rejects concurrent prompt", func(t *testing.T) {
		sessionID := acp.SessionId("active-session")
		agent := &Agent{
			activeSessions: new(cpesync.Map[acp.SessionId, *cpesync.Guard[session]]),
			runtimeFactory: runtimeCreatorFunc(func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				t.Fatal("runtime should not be created for an active session")
				return nil, nil
			}),
		}
		agent.activeSessions.Store(sessionID, cpesync.NewGuard(session{
			runtime:    testRuntime{},
			cancelfunc: func() {},
		}))

		_, err := agent.Prompt(t.Context(), &acp.PromptRequest{
			SessionID: sessionID,
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("second prompt")},
		})
		be.True(t, err != nil)
		be.Equal(t, err.Error(), "cannot do prompt turn in actively generating session")
	})
	t.Run("panics when another process modifies the same session", func(t *testing.T) {
		// This represents an invalid deployment in which two CPE ACP processes are
		// serving the same persisted session. Each Agent has its own SQLite handle
		// and in-memory prompt guard, so both can read the same session head before
		// either process advances it. The rendezvous below makes that race
		// deterministic. Storage must detect the stale head with its optimistic
		// compare-and-swap, and ACP must panic because concurrent ownership of one
		// session is a fatal setup error rather than a recoverable prompt outcome.
		// The test recovers that panic only so it can verify ErrSessionConflict;
		// pruning messages or cost persisted by the losing process is intentionally
		// outside prompt finalization and can be handled by a future maintenance
		// command.
		dbPath := filepath.Join(t.TempDir(), "conversations.db")
		stores := make([]*storage.Sqlite, 0, 2)
		for range 2 {
			store, err := storage.NewConvoDB(t.Context(), dbPath)
			be.Err(t, err, nil)
			t.Cleanup(func() { _ = store.Close() })
			stores = append(stores, store)
		}

		sessionID := acp.SessionId("shared-session")
		cwd := t.TempDir()
		be.Err(t, stores[0].CreateACPSession(t.Context(), storage.CreateACPSessionParams{
			Session: acp.SessionInfo{
				Cwd:       cwd,
				SessionID: sessionID,
			},
			ModelRef: "test-model",
		}), nil)

		ready := make(chan struct{}, 2)
		release := make(chan struct{})
		newRuntime := func(store *storage.Sqlite, answer string) runtime {
			return &closeTrackingRuntime{generate: func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
				ready <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return dialog, ctx.Err()
				}

				dialog = append(dialog, gai.Message{
					Role:   gai.Assistant,
					Blocks: []gai.Block{gai.TextBlock(answer)},
				})
				idx := 0
				for saved, err := range store.SaveDialog(ctx, slices.Values(dialog)) {
					if err != nil {
						return dialog, err
					}
					dialog[idx] = saved
					idx++
				}
				if _, err := store.AddACPSessionCost(ctx, sessionID, 1); err != nil {
					return dialog, err
				}
				return dialog, nil
			}}
		}
		newAgent := func(store *storage.Sqlite, r runtime) *Agent {
			agent := &Agent{
				activeSessions: new(cpesync.Map[acp.SessionId, *cpesync.Guard[session]]),
				rawCfg:         &config.RawConfig{},
				skillHomeDir:   t.TempDir(),
				db:             store,
			}
			agent.activeSessions.Store(sessionID, cpesync.NewGuard(session{
				id:      sessionID,
				cwd:     cwd,
				model:   "test-model",
				runtime: r,
			}))
			return agent
		}

		agents := []*Agent{
			newAgent(stores[0], newRuntime(stores[0], "first answer")),
			newAgent(stores[1], newRuntime(stores[1], "second answer")),
		}
		type promptOutcome struct {
			err        error
			panicValue any
		}
		outcomes := make(chan promptOutcome, len(agents))
		var prompts sync.WaitGroup
		for _, agent := range agents {
			prompts.Go(func() {
				outcome := promptOutcome{}
				func() {
					defer func() { outcome.panicValue = recover() }()
					_, outcome.err = agent.Prompt(t.Context(), &acp.PromptRequest{
						SessionID: sessionID,
						Prompt:    []acp.ContentBlock{acp.TextContentBlock("concurrent prompt")},
					})
				}()
				outcomes <- outcome
			})
		}
		for range agents {
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("timed out waiting for both prompts to observe the same session head")
			}
		}
		close(release)
		prompts.Wait()
		close(outcomes)

		var succeeded, panicked int
		for outcome := range outcomes {
			if outcome.panicValue != nil {
				panicErr, ok := outcome.panicValue.(error)
				if !ok {
					t.Fatalf("Prompt panic = %#v, want an error", outcome.panicValue)
				}
				if !errors.Is(panicErr, storage.ErrSessionConflict) {
					t.Fatalf("Prompt panic = %v, want ErrSessionConflict", panicErr)
				}
				panicked++
				continue
			}
			if outcome.err != nil {
				t.Fatalf("Prompt returned error instead of panicking: %v", outcome.err)
			}
			succeeded++
		}
		be.Equal(t, succeeded, 1)
		be.Equal(t, panicked, 1)

		storedSession, err := stores[0].GetACPSession(t.Context(), sessionID)
		be.Err(t, err, nil)
		winningDialog, err := storage.GetDialogForMessage(t.Context(), stores[0], storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(winningDialog), 2)
	})
	t.Run("maps max generation limit to stop reason", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
				Models: []config.ModelConfig{{
					Model: config.Model{
						Ref:                  "test-model",
						DisplayName:          "Test Model",
						ID:                   "test-model",
						Type:                 "responses",
						ContextWindow:        100,
						InputCostPerMillion:  new(1.0),
						OutputCostPerMillion: new(1.0),
					},
				}},
			}
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{responses: []genFunc{
					func(ctx context.Context, dialog gai.Dialog, genOpts *gai.GenOpts) (gai.Response, error) {
						be.Equal(t, len(dialog), 1)
						be.Equal(t, dialog[0].Role, gai.User)
						be.Equal(t, dialog[0].Blocks[0].Content.String(), "Hello")
						return gai.Response{}, gai.ErrMaxGenerationLimit
					},
				}}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonMaxTokens)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, len(testClient.notifications()), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("maps content policy error to refusal", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
				Models: []config.ModelConfig{{
					Model: config.Model{
						Ref:                  "test-model",
						DisplayName:          "Test Model",
						ID:                   "test-model",
						Type:                 "responses",
						ContextWindow:        100,
						InputCostPerMillion:  new(1.0),
						OutputCostPerMillion: new(1.0),
					},
				}},
			}
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{responses: []genFunc{
					func(ctx context.Context, dialog gai.Dialog, genOpts *gai.GenOpts) (gai.Response, error) {
						be.Equal(t, len(dialog), 1)
						be.Equal(t, dialog[0].Role, gai.User)
						be.Equal(t, dialog[0].Blocks[0].Content.String(), "Hello")
						return gai.Response{}, gai.ContentPolicyErr("blocked")
					},
				}}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonRefusal)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, len(testClient.notifications()), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("maps cancelled generation to stop reason", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
				Models: []config.ModelConfig{{
					Model: config.Model{
						Ref:                  "test-model",
						DisplayName:          "Test Model",
						ID:                   "test-model",
						Type:                 "responses",
						ContextWindow:        100,
						InputCostPerMillion:  new(1.0),
						OutputCostPerMillion: new(1.0),
					},
				}},
			}
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{responses: []genFunc{
					func(ctx context.Context, dialog gai.Dialog, genOpts *gai.GenOpts) (gai.Response, error) {
						be.Equal(t, len(dialog), 1)
						be.Equal(t, dialog[0].Role, gai.User)
						be.Equal(t, dialog[0].Blocks[0].Content.String(), "Hello")
						return gai.Response{}, context.Canceled
					},
				}}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonCancelled)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, len(testClient.notifications()), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("surfaces unknown generation error", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
				Models: []config.ModelConfig{{
					Model: config.Model{
						Ref:                  "test-model",
						DisplayName:          "Test Model",
						ID:                   "test-model",
						Type:                 "responses",
						ContextWindow:        100,
						InputCostPerMillion:  new(1.0),
						OutputCostPerMillion: new(1.0),
					},
				}},
			}
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{responses: []genFunc{
					func(ctx context.Context, dialog gai.Dialog, genOpts *gai.GenOpts) (gai.Response, error) {
						be.Equal(t, len(dialog), 1)
						be.Equal(t, dialog[0].Role, gai.User)
						be.Equal(t, dialog[0].Blocks[0].Content.String(), "Hello")
						return gai.Response{}, errors.New("boom")
					},
				}}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		_, err = clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
			SessionID: newSessionResp.SessionID,
		})
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), "unknown error while generating: boom"))
		be.Equal(t, len(testClient.notifications()), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("omits prompt usage and usage notification without metadata", func(t *testing.T) {
		var (
			clientConn *acp.Client
			store      *storage.Sqlite
			cwd        = t.TempDir()
			testClient = &promptTestClient{}
			rawCfg     = config.RawConfig{
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
		)
		fixture := setup(
			t,
			testClient,
			&rawCfg,
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
				gen := testGen{
					responses: []genFunc{
						func(ctx context.Context, d gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
							return gai.Response{
								Candidates: []gai.Message{{
									Role:            gai.Assistant,
									Blocks:          []gai.Block{gai.TextBlock("metadata-free answer")},
									ToolResultError: false,
									ExtraFields:     map[string]any{},
								}},
								FinishReason: gai.EndTurn,
							}, nil
						},
					},
				}
				cfg, err := config.ResolveFromRaw(&rawCfg, config.RuntimeOptions{ModelRef: s.model})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:     &gen,
					Store: store,
					Cfg:   cfg,
					conn:  conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(1),
			ClientCapabilities: &acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), &acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextContentBlock("Hello")},
			SessionID: newSessionResp.SessionID,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, promptResp.Usage, nil)
		assertNotifications(t, testClient, []acp.SessionNotification{
			{
				SessionID: newSessionResp.SessionID,
				Update:    expectedRPCAgentMessageChunk("metadata-free answer"),
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		storedAssistant := storedDialog[len(storedDialog)-1]
		_, ok := storedAssistant.ExtraFields[storage.AgentMetadataInputTokensKey]
		be.True(t, !ok)
		_, ok = storedAssistant.ExtraFields[storage.AgentMetadataOutputTokensKey]
		be.True(t, !ok)
	})
}
