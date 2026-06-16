package acp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/nalgeon/be"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	cpesync "github.com/spachava753/cpe/internal/sync"
)

type promptTestClient struct {
	noOpAcpClient
	capturedNotifications []acp.SessionNotification
}

// SessionUpdate implements [acp.Client].
func (t *promptTestClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	t.capturedNotifications = append(t.capturedNotifications, params)
	return nil
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

func TestPrompt(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		var (
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ClientCapabilities: acp.ClientCapabilities{
				Fs: acp.FileSystemCapabilities{
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
		be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 90, InputTokens: 80, OutputTokens: 10})
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
			{
				SessionId: sessionId,
				Update: acp.SessionUpdate{
					UsageUpdate: &acp.SessionUsageUpdate{
						Cost: &acp.Cost{
							Amount:   0.00009,
							Currency: "USD",
						},
						SessionUpdate: "usage_update",
						Size:          100,
						Used:          90,
					},
				},
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
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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
					G:           gen,
					DialogSaver: store,
					CostAdder:   store,
					Cfg:         cfg,
					conn:        conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ClientCapabilities: acp.ClientCapabilities{
				Fs: acp.FileSystemCapabilities{
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
		be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 7, InputTokens: 5, OutputTokens: 2})
		be.True(t, gen != nil)
		be.Equal(t, gen.called, 2)
		be.Equal(t, testClient.capturedNotifications, []acp.SessionNotification{
			{
				SessionId: sessionId,
				Update: acp.SessionUpdate{
					ToolCall: &acp.SessionUpdateToolCall{
						RawInput: map[string]any{
							"summary": "conversation compacted state",
						},
						SessionUpdate: "tool_call",
						Status:        acp.ToolCallStatusPending,
						Title:         config.CompactionToolName,
						ToolCallId:    "compact-call-1",
					},
				},
			},
			{
				SessionId: sessionId,
				Update: acp.SessionUpdate{
					UsageUpdate: &acp.SessionUsageUpdate{
						Cost: &acp.Cost{
							Amount:   0.00009,
							Currency: "USD",
						},
						SessionUpdate: "usage_update",
						Size:          100,
						Used:          90,
					},
				},
			},
			{
				SessionId: sessionId,
				Update: acp.SessionUpdate{
					UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
						Content: acp.ContentBlock{
							Text: &acp.ContentBlockText{
								Text: "compacted conversation: conversation compacted state",
								Type: "text",
							},
						},
						SessionUpdate: "user_message_chunk",
					},
				},
			},
			{
				SessionId: sessionId,
				Update: acp.SessionUpdate{
					AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
						Content: acp.ContentBlock{
							Text: &acp.ContentBlockText{
								Text: "continued after compaction",
								Type: "text",
							},
						},
						SessionUpdate: "agent_message_chunk",
					},
				},
			},
			{
				SessionId: sessionId,
				Update: acp.SessionUpdate{
					UsageUpdate: &acp.SessionUsageUpdate{
						Cost: &acp.Cost{
							Amount:   0.000097,
							Currency: "USD",
						},
						SessionUpdate: "usage_update",
						Size:          100,
						Used:          7,
					},
				},
			},
		})
		firstUsage := testClient.capturedNotifications[1].Update.UsageUpdate
		secondUsage := testClient.capturedNotifications[4].Update.UsageUpdate
		be.True(t, firstUsage != nil)
		be.True(t, secondUsage != nil)
		be.True(t, firstUsage.Used > secondUsage.Used)

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
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
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
		_, err = store.AddACPSessionMessage(t.Context(), newSessionResp.SessionId, lastMessageID)
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("follow-up")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 16, InputTokens: 12, OutputTokens: 4})
		be.Equal(t, testClient.capturedNotifications, []acp.SessionNotification{
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
						Content:       acp.TextBlock("continued answer"),
						SessionUpdate: "agent_message_chunk",
					},
				},
			},
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					UsageUpdate: &acp.SessionUsageUpdate{
						Cost: &acp.Cost{
							Amount:   0.000016,
							Currency: "USD",
						},
						SessionUpdate: "usage_update",
						Size:          100,
						Used:          16,
					},
				},
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
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
			clientConn  *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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
					G:           gen,
					DialogSaver: store,
					CostAdder:   store,
					Cfg:         cfg,
					conn:        conn,
				}}, nil
			},
		)
		clientConn = fixture.ClientConn
		store = fixture.Store

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		firstResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("first prompt")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, firstResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, firstResp.Usage, &acp.Usage{TotalTokens: 7, InputTokens: 5, OutputTokens: 2})

		secondResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("second prompt")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, secondResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, secondResp.Usage, &acp.Usage{TotalTokens: 9, InputTokens: 6, OutputTokens: 3})
		be.Equal(t, factoryCall, 1)
		be.True(t, gen != nil)
		be.Equal(t, gen.called, 2)
		firstCost := float64(5)/1_000_000 + float64(2)/1_000_000
		secondCost := firstCost + float64(6)/1_000_000 + float64(3)/1_000_000
		be.Equal(t, testClient.capturedNotifications, []acp.SessionNotification{
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
						Content:       acp.TextBlock("first answer"),
						SessionUpdate: "agent_message_chunk",
					},
				},
			},
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					UsageUpdate: &acp.SessionUsageUpdate{
						Cost: &acp.Cost{
							Amount:   firstCost,
							Currency: "USD",
						},
						SessionUpdate: "usage_update",
						Size:          100,
						Used:          7,
					},
				},
			},
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
						Content:       acp.TextBlock("second answer"),
						SessionUpdate: "agent_message_chunk",
					},
				},
			},
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					UsageUpdate: &acp.SessionUsageUpdate{
						Cost: &acp.Cost{
							Amount:   secondCost,
							Currency: "USD",
						},
						SessionUpdate: "usage_update",
						Size:          100,
						Used:          9,
					},
				},
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
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
			runtimeFactory: runtimeCreatorFunc(func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
				t.Fatal("runtime should not be created for an active session")
				return nil, nil
			}),
		}
		agent.activeSessions.Store(sessionID, cpesync.NewGuard(session{
			runtime:    testRuntime{},
			cancelfunc: func() {},
		}))

		_, err := agent.Prompt(t.Context(), acp.PromptRequest{
			SessionId: sessionID,
			Prompt:    []acp.ContentBlock{acp.TextBlock("second prompt")},
		})
		be.True(t, err != nil)
		be.Equal(t, err.Error(), "cannot do prompt turn in actively generating session")
	})
	t.Run("maps max generation limit to stop reason", func(t *testing.T) {
		var (
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("Hello")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonMaxTokens)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, len(testClient.capturedNotifications), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("maps content policy error to refusal", func(t *testing.T) {
		var (
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("Hello")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonRefusal)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, len(testClient.capturedNotifications), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("maps cancelled generation to stop reason", func(t *testing.T) {
		var (
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("Hello")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonCancelled)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, len(testClient.capturedNotifications), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("surfaces unknown generation error", func(t *testing.T) {
		var (
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		be.Err(t, err, nil)

		_, err = clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("Hello")},
			SessionId: newSessionResp.SessionId,
		})
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), "unknown error while generating: boom"))
		be.Equal(t, len(testClient.capturedNotifications), 0)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		storedDialog, err := storage.GetDialogForMessage(t.Context(), store, storedSession.LastMessageID)
		be.Err(t, err, nil)
		be.Equal(t, len(storedDialog), 1)
		be.Equal(t, storedDialog[0].Role, gai.User)
		be.Equal(t, storedDialog[0].Blocks[0].Content.String(), "Hello")
	})
	t.Run("omits prompt usage and usage notification without metadata", func(t *testing.T) {
		var (
			clientConn *acp.ClientSideConnection
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
			func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentSideConnection) (runtime, error) {
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

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersionNumber,
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
		})
		be.Err(t, err, nil)
		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt:    []acp.ContentBlock{acp.TextBlock("Hello")},
			SessionId: newSessionResp.SessionId,
		})
		be.Err(t, err, nil)
		be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
		be.Equal(t, promptResp.Usage, nil)
		be.Equal(t, testClient.capturedNotifications, []acp.SessionNotification{
			{
				SessionId: newSessionResp.SessionId,
				Update: acp.SessionUpdate{
					AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
						Content:       acp.TextBlock("metadata-free answer"),
						SessionUpdate: "agent_message_chunk",
					},
				},
			},
		})

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
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
