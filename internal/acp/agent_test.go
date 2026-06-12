package acp

import (
	"context"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/nalgeon/be"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
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
			func(ctx context.Context, opts runtimeOpts) (runtime, error) {
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
					ModelRef: "test-model",
					// TODO: Should we set timeout?
					// TODO: Should we set gen opts?
				})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:           &gen,
					DialogSaver: store,
					CostAdder:   store,
					Cfg:         cfg,
					conn:        opts.conn,
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
			func(ctx context.Context, opts runtimeOpts) (runtime, error) {
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
					ModelRef: "test-model",
					// TODO: Should we set timeout?
					// TODO: Should we set gen opts?
				})
				be.Err(t, err, nil)
				return testRuntime{Loop: &Loop{
					G:           gen,
					DialogSaver: store,
					CostAdder:   store,
					Cfg:         cfg,
					conn:        opts.conn,
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
}
