package acp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

func TestSessionConfigOptions(t *testing.T) {
	t.Run("new session waits for model before showing thinking levels", func(t *testing.T) {
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
		be.Equal(t, len(*resp.ConfigOptions), 1)

		modelOption := (*resp.ConfigOptions)[0]
		be.Equal(t, modelOption.Type, acp.SessionConfigOptionTypeSelect)
		be.Equal(t, modelOption.ID, modelRefConfigId)
		be.Equal(t, modelOption.Name, "Model")
		be.Equal(t, *modelOption.Category, acp.SessionConfigOptionCategoryModel)
		be.Equal(t, modelOption.CurrentValue, any(""))
		be.Equal(t, len(*modelOption.Options.Ungrouped), 2)
		be.Equal(t, (*modelOption.Options.Ungrouped)[0].Value, acp.SessionConfigValueId("test-model"))
		be.Equal(t, (*modelOption.Options.Ungrouped)[0].Name, "Test Model")
		be.Equal(t, (*modelOption.Options.Ungrouped)[1].Value, acp.SessionConfigValueId("test-model2"))
		be.Equal(t, (*modelOption.Options.Ungrouped)[1].Name, "Test Model 2")
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

		_, err := clientConn.Initialize(t.Context(), &acp.InitializeRequest{
			ClientCapabilities: &acp.ClientCapabilities{
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

		resp, err := clientConn.NewSession(t.Context(), &acp.NewSessionRequest{
			Cwd:        "/rando/dir",
			McpServers: []acp.McpServer{},
		})
		be.Err(t, err, nil)
		be.Equal(t, len(*resp.ConfigOptions), 1)

		modelOption := (*resp.ConfigOptions)[0]
		be.Equal(t, modelOption.Type, acp.SessionConfigOptionTypeSelect)
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

		setResp, err := clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model2",
		})
		be.Err(t, err, nil)
		be.Equal(t, len(setResp.ConfigOptions), 2)
		be.Equal(t, setResp.ConfigOptions[0].ID, modelRefConfigId)
		be.Equal(t, setResp.ConfigOptions[0].CurrentValue, any("test-model2"))
		be.Equal(t, setResp.ConfigOptions[1].ID, thinkingLevelConfigId)
		be.Equal(t, setResp.ConfigOptions[1].CurrentValue, any("medium"))
		be.Equal(t, len(*setResp.ConfigOptions[1].Options.Ungrouped), 2)
		be.Equal(t, (*setResp.ConfigOptions[1].Options.Ungrouped)[0].Value, acp.SessionConfigValueId("medium"))
		be.Equal(t, (*setResp.ConfigOptions[1].Options.Ungrouped)[1].Value, acp.SessionConfigValueId("deep"))

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
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

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "test-model",
		})
		be.Err(t, err, nil)

		setResp, err := clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  thinkingLevelConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "high",
		})
		be.Err(t, err, nil)
		be.Equal(t, len(setResp.ConfigOptions), 2)
		be.Equal(t, setResp.ConfigOptions[0].ID, modelRefConfigId)
		be.Equal(t, setResp.ConfigOptions[0].CurrentValue, any("test-model"))
		be.Equal(t, setResp.ConfigOptions[1].ID, thinkingLevelConfigId)
		be.Equal(t, setResp.ConfigOptions[1].CurrentValue, any("high"))

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
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

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  "unknown",
			SessionID: newSessionResp.SessionID,
			Value:     "anything",
		})
		be.True(t, err != nil)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "")
		be.Equal(t, storedSession.ThinkingLevel, "")
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

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "missing-model",
		})
		be.True(t, err != nil)

		_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
			ConfigID:  thinkingLevelConfigId,
			SessionID: newSessionResp.SessionID,
			Value:     "missing-thinking",
		})
		be.True(t, err != nil)

		storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
		be.Err(t, err, nil)
		be.Equal(t, storedSession.ModelRef, "")
		be.Equal(t, storedSession.ThinkingLevel, "")
	})
}

func TestSetSessionConfigOptionDuringPrompt(t *testing.T) {
	generateStarted := make(chan struct{})
	releaseGenerate := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	var clientConn *acp.Client
	var store *storage.Sqlite
	var capturedThinkingBudgets []string

	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseGenerate)
		})
	})

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
	}
	testClient := &promptTestClient{}
	fixture := setup(
		t,
		testClient,
		&rawCfg,
		func(ctx context.Context, s session, caps acp.ClientCapabilities, conn *acp.AgentConnection) (runtime, error) {
			gen := testGen{responses: []genFunc{
				func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
					if opts == nil {
						capturedThinkingBudgets = append(capturedThinkingBudgets, "")
					} else {
						capturedThinkingBudgets = append(capturedThinkingBudgets, opts.ThinkingBudget)
					}
					startedOnce.Do(func() {
						close(generateStarted)
					})
					<-releaseGenerate

					return gai.Response{
						Candidates: []gai.Message{{
							Role:            gai.Assistant,
							Blocks:          []gai.Block{gai.TextBlock("done")},
							ToolResultError: false,
							ExtraFields:     map[string]any{},
						}},
						FinishReason: gai.EndTurn,
					}, nil
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

	_, err = clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
		ConfigID:  modelRefConfigId,
		SessionID: newSessionResp.SessionID,
		Value:     "test-model",
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
	be.Equal(t, capturedThinkingBudgets, []string{"low"})

	setResp, err := clientConn.SetSessionConfigOption(t.Context(), &acp.SetSessionConfigOptionRequest{
		ConfigID:  thinkingLevelConfigId,
		SessionID: newSessionResp.SessionID,
		Value:     "high",
	})
	be.Err(t, err, nil)
	be.Equal(t, len(setResp.ConfigOptions), 2)
	be.Equal(t, setResp.ConfigOptions[1].ID, thinkingLevelConfigId)
	be.Equal(t, setResp.ConfigOptions[1].CurrentValue, any("high"))

	storedSession, err := store.GetACPSession(t.Context(), newSessionResp.SessionID)
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
