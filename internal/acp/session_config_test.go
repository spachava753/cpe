package acp

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

func TestSessionConfigOptions(t *testing.T) {
	t.Run("new session returns ordered defaults", func(t *testing.T) {
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
					{
						Model: config.Model{
							Ref:                  "test-model2",
							DisplayName:          "Test Model 2",
							ID:                   "test-model2",
							Type:                 "responses",
							BaseUrl:              "https://customurl.com/v1",
							ContextWindow:        200,
							InputCostPerMillion:  new(2.0),
							OutputCostPerMillion: new(2.0),
						},
					},
				},
			},
			unreachableRuntimeFactory,
		)
		clientConn := fixture.ClientConn

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
		be.Err(t, err, nil)

		resp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)
		be.Equal(t, len(resp.ConfigOptions), 2)

		modelOption := resp.ConfigOptions[0].Select
		be.True(t, modelOption != nil)
		be.Equal(t, modelOption.Id, modelRefConfigId)
		be.Equal(t, modelOption.Name, "Model")
		be.Equal(t, *modelOption.Category, acp.SessionConfigOptionCategoryModel)
		be.Equal(t, modelOption.Type, "select")
		be.Equal(t, modelOption.CurrentValue, acp.SessionConfigValueId("test-model"))
		be.Equal(t, len(*modelOption.Options.Ungrouped), 2)
		be.Equal(t, (*modelOption.Options.Ungrouped)[0].Value, acp.SessionConfigValueId("test-model"))
		be.Equal(t, (*modelOption.Options.Ungrouped)[0].Name, "Test Model")
		be.Equal(t, (*modelOption.Options.Ungrouped)[1].Value, acp.SessionConfigValueId("test-model2"))
		be.Equal(t, (*modelOption.Options.Ungrouped)[1].Name, "Test Model 2")

		thinkingOption := resp.ConfigOptions[1].Select
		be.True(t, thinkingOption != nil)
		be.Equal(t, thinkingOption.Id, thinkingLevelConfigId)
		be.Equal(t, thinkingOption.Name, "Thinking level")
		be.Equal(t, *thinkingOption.Category, acp.SessionConfigOptionCategoryThoughtLevel)
		be.Equal(t, thinkingOption.Type, "select")
		be.Equal(t, thinkingOption.CurrentValue, acp.SessionConfigValueId("low"))
		be.Equal(t, len(*thinkingOption.Options.Ungrouped), 2)
		be.Equal(t, (*thinkingOption.Options.Ungrouped)[0].Value, acp.SessionConfigValueId("low"))
		be.Equal(t, (*thinkingOption.Options.Ungrouped)[0].Name, "Low")
		be.Equal(t, (*thinkingOption.Options.Ungrouped)[1].Value, acp.SessionConfigValueId("high"))
		be.Equal(t, (*thinkingOption.Options.Ungrouped)[1].Name, "High")
	})

	t.Run("new session succeeds when model has no costs defined", func(t *testing.T) {
		fixture := setup(
			t,
			&noOpAcpClient{},
			&config.RawConfig{
				Models: []config.ModelConfig{
					{
						Model: config.Model{
							Ref:           "free-model",
							DisplayName:   "Free Model",
							ID:            "free-model",
							Type:          "responses",
							BaseUrl:       "https://customurl.com/v1",
							ContextWindow: 100,
						},
					},
				},
			},
			unreachableRuntimeFactory,
		)
		clientConn := fixture.ClientConn

		_, err := clientConn.Initialize(t.Context(), acp.InitializeRequest{
			ClientCapabilities: acp.ClientCapabilities{
				Terminal: true,
			},
			ClientInfo: &acp.Implementation{
				Name:    "test-client",
				Title:   new("test client"),
				Version: "test",
			},
			ProtocolVersion: acp.ProtocolVersionNumber,
		})
		be.Err(t, err, nil)

		resp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)
		be.Equal(t, len(resp.ConfigOptions), 1)

		modelOption := resp.ConfigOptions[0].Select
		be.True(t, modelOption != nil)
		be.Equal(t, len(*modelOption.Options.Ungrouped), 1)
		desc := (*modelOption.Options.Ungrouped)[0].Description
		be.True(t, desc != nil)
		be.True(t, strings.Contains(*desc, "Input Cost: n/a"))
		be.True(t, strings.Contains(*desc, "Output Cost: n/a"))
	})
}

func TestSetSessionConfigOption(t *testing.T) {
	t.Run("set model returns complete dependent state", func(t *testing.T) {
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
					{
						Model: config.Model{
							Ref:                  "test-model2",
							DisplayName:          "Test Model 2",
							ID:                   "test-model2",
							Type:                 "responses",
							BaseUrl:              "https://customurl.com/v1",
							ContextWindow:        200,
							InputCostPerMillion:  new(2.0),
							OutputCostPerMillion: new(2.0),
							ThinkingValues: []config.ThinkingValueConfig{
								{
									Name:        "Medium",
									Value:       "medium",
									Description: "Medium thinking",
								},
								{
									Name:        "Deep",
									Value:       "deep",
									Description: "Deep thinking",
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
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		setResp, err := clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId:  modelRefConfigId,
				SessionId: newSessionResp.SessionId,
				Value:     "test-model2",
			},
		})
		be.Err(t, err, nil)
		be.Equal(t, len(setResp.ConfigOptions), 2)
		be.Equal(t, setResp.ConfigOptions[0].Select.Id, modelRefConfigId)
		be.Equal(t, setResp.ConfigOptions[0].Select.CurrentValue, acp.SessionConfigValueId("test-model2"))
		be.Equal(t, setResp.ConfigOptions[1].Select.Id, thinkingLevelConfigId)
		be.Equal(t, setResp.ConfigOptions[1].Select.CurrentValue, acp.SessionConfigValueId("medium"))
		be.Equal(t, len(*setResp.ConfigOptions[1].Select.Options.Ungrouped), 2)
		be.Equal(t, (*setResp.ConfigOptions[1].Select.Options.Ungrouped)[0].Value, acp.SessionConfigValueId("medium"))
		be.Equal(t, (*setResp.ConfigOptions[1].Select.Options.Ungrouped)[1].Value, acp.SessionConfigValueId("deep"))

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model2")
		be.Equal(t, storedSession.ThinkingLevel, "medium")
	})

	t.Run("set thinking level returns complete state", func(t *testing.T) {
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
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		setResp, err := clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId:  thinkingLevelConfigId,
				SessionId: newSessionResp.SessionId,
				Value:     "high",
			},
		})
		be.Err(t, err, nil)
		be.Equal(t, len(setResp.ConfigOptions), 2)
		be.Equal(t, setResp.ConfigOptions[0].Select.Id, modelRefConfigId)
		be.Equal(t, setResp.ConfigOptions[0].Select.CurrentValue, acp.SessionConfigValueId("test-model"))
		be.Equal(t, setResp.ConfigOptions[1].Select.Id, thinkingLevelConfigId)
		be.Equal(t, setResp.ConfigOptions[1].Select.CurrentValue, acp.SessionConfigValueId("high"))

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model")
		be.Equal(t, storedSession.ThinkingLevel, "high")
	})

	t.Run("rejects unknown config id", func(t *testing.T) {
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
			unreachableRuntimeFactory,
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

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
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId:  "unknown",
				SessionId: newSessionResp.SessionId,
				Value:     "anything",
			},
		})
		be.True(t, err != nil)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model")
		be.Equal(t, storedSession.ThinkingLevel, "low")
	})

	t.Run("rejects values outside option list", func(t *testing.T) {
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
			unreachableRuntimeFactory,
		)
		clientConn := fixture.ClientConn
		store := fixture.Store

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
		be.Err(t, err, nil)

		newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId:  modelRefConfigId,
				SessionId: newSessionResp.SessionId,
				Value:     "missing-model",
			},
		})
		be.True(t, err != nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId:  thinkingLevelConfigId,
				SessionId: newSessionResp.SessionId,
				Value:     "missing-thinking",
			},
		})
		be.True(t, err != nil)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "test-model")
		be.Equal(t, storedSession.ThinkingLevel, "low")
	})
}

func TestSetSessionConfigOptionDuringPrompt(t *testing.T) {
	generateStarted := make(chan struct{})
	releaseGenerate := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	var clientConn *acp.ClientSideConnection
	var store *storage.Sqlite
	var capturedThinkingBudgets []string

	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseGenerate)
		})
	})

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
		func(opts runtimeOpts) (acpRuntime, error) {
			return mockRuntime(func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Dialog, error) {
				if opts == nil {
					capturedThinkingBudgets = append(capturedThinkingBudgets, "")
				} else {
					capturedThinkingBudgets = append(capturedThinkingBudgets, opts.ThinkingBudget)
				}
				startedOnce.Do(func() {
					close(generateStarted)
				})
				<-releaseGenerate

				generatedDialog := gai.Dialog{
					{
						Role:   gai.Assistant,
						Blocks: []gai.Block{gai.TextBlock("done")},
					},
				}
				savedDialog := make(gai.Dialog, 0, len(generatedDialog))
				for msg, err := range store.SaveDialog(context.WithoutCancel(ctx), slices.Values(generatedDialog)) {
					if err != nil {
						return nil, err
					}
					savedDialog = append(savedDialog, msg)
				}
				return savedDialog, nil
			}), nil
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
	be.Err(t, err, nil)

	newSessionResp, err := clientConn.NewSession(t.Context(), acp.NewSessionRequest{
		Cwd:        "/rando/dir",
		McpServers: []acp.McpServer{},
	})
	be.Err(t, err, nil)

	type promptResult struct {
		resp acp.PromptResponse
		err  error
	}
	promptResultCh := make(chan promptResult, 1)
	go func() {
		resp, err := clientConn.Prompt(t.Context(), acp.PromptRequest{
			Prompt: []acp.ContentBlock{
				{
					Text: &acp.ContentBlockText{
						Text: "Hello",
						Type: "text",
					},
				},
			},
			SessionId: newSessionResp.SessionId,
		})
		promptResultCh <- promptResult{resp: resp, err: err}
	}()

	select {
	case <-generateStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime generation to start")
	}
	be.Equal(t, capturedThinkingBudgets, []string{"low"})

	setResp, err := clientConn.SetSessionConfigOption(t.Context(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			ConfigId:  thinkingLevelConfigId,
			SessionId: newSessionResp.SessionId,
			Value:     "high",
		},
	})
	be.Err(t, err, nil)
	be.Equal(t, len(setResp.ConfigOptions), 2)
	be.Equal(t, setResp.ConfigOptions[1].Select.Id, thinkingLevelConfigId)
	be.Equal(t, setResp.ConfigOptions[1].Select.CurrentValue, acp.SessionConfigValueId("high"))

	storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionId)
	be.Err(t, err, nil)
	be.Equal(t, storedSession.ThinkingLevel, "high")

	releaseOnce.Do(func() {
		close(releaseGenerate)
	})

	select {
	case result := <-promptResultCh:
		be.Err(t, result.err, nil)
		be.Equal(t, result.resp.StopReason, acp.StopReasonEndTurn)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt to return")
	}
}
