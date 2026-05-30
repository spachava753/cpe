package acp

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
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

func TestPrompt(t *testing.T) {
	var (
		clientConn *acp.ClientSideConnection
		store      *storage.Sqlite
		cwd        = t.TempDir()
		testClient = &promptTestClient{}
	)
	clientConn, store = setup(
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
