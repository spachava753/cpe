package acp

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
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

type testRuntime struct {
	*Loop
}

func (r testRuntime) Close() error {
	return nil
}

type testGen struct {
	delay    time.Duration
	response gai.Response
}

func (g testGen) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	if g.delay > 0 {
		time.Sleep(g.delay)
	}
	return g.response, nil
}

func (g testGen) Register(tool gai.Tool) error {
	return nil
}

var _ gai.ToolCallingGenerator = (*testGen)(nil)

func TestPrompt(t *testing.T) {
	var (
		clientConn *acp.ClientSideConnection
		store      *storage.Sqlite
		cwd        = t.TempDir()
		testClient = &promptTestClient{}
	)
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
		func(ctx context.Context, opts runtimeOpts) (runtime, error) {
			gen := testGen{response: gai.Response{
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
			}}
			return testRuntime{Loop: &Loop{
				G:           gen,
				DialogSaver: store,
				CostAdder:   store,
				Cfg: config.Config{Model: config.Model{
					Ref:                  "test-model",
					DisplayName:          "Test Model",
					ID:                   "test-model",
					Type:                 "responses",
					BaseUrl:              "https://customurl.com/v1",
					ContextWindow:        100,
					InputCostPerMillion:  new(1.0),
					OutputCostPerMillion: new(1.0),
				}},
				conn: opts.conn,
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
}

func TestPromptHandlesCompactedDialogShorterThanInputDialog(t *testing.T) {
	var store *storage.Sqlite
	fixture := setup(
		t,
		&noOpAcpClient{},
		&config.RawConfig{
			Models: []config.ModelConfig{{
				Model: config.Model{
					Ref:                  "test-model",
					DisplayName:          "Test Model",
					ID:                   "test-model",
					Type:                 "responses",
					InputCostPerMillion:  new(1.0),
					OutputCostPerMillion: new(1.0),
				},
			}},
		},
		func(ctx context.Context, opts runtimeOpts) (runtime, error) {
			return mockRuntime(func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
				compacted := gai.Dialog{
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("compacted")}},
					{
						Role:   gai.Assistant,
						Blocks: []gai.Block{gai.TextBlock("done")},
						ExtraFields: map[string]any{
							storage.AgentMetadataInputTokensKey:  int64(5),
							storage.AgentMetadataOutputTokensKey: int64(2),
						},
					},
				}

				var saved gai.Dialog
				for msg, err := range store.SaveDialog(ctx, slices.Values(compacted)) {
					if err != nil {
						return nil, err
					}
					saved = append(saved, msg)
				}
				return saved, nil
			}), nil
		},
	)
	clientConn := fixture.ClientConn
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

	seed := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("first")}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("second")}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("third")}},
	}
	var lastMessageID string
	for msg, err := range store.SaveDialog(t.Context(), slices.Values(seed)) {
		be.Err(t, err, nil)
		lastMessageID = storage.GetMessageID(msg)
	}
	_, err = store.AddACPSessionMessage(t.Context(), newSessionResp.SessionId, lastMessageID)
	be.Err(t, err, nil)

	promptResp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
		Prompt:    []acp.ContentBlock{acp.TextBlock("continue")},
		SessionId: newSessionResp.SessionId,
	})
	be.Err(t, err, nil)
	be.Equal(t, promptResp.StopReason, acp.StopReasonEndTurn)
	be.Equal(t, promptResp.Usage, &acp.Usage{TotalTokens: 7, InputTokens: 5, OutputTokens: 2})
}
