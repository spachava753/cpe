package client

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
)

type fakeAgent struct {
	t                         *testing.T
	modelOptions              acp.UngroupedSessionConfigSelectOptions
	firstModelThinkingOptions acp.UngroupedSessionConfigSelectOptions
	setModelThinkingOptions   acp.UngroupedSessionConfigSelectOptions
	setModelHasNoThinking     bool
	setRequests               []acp.SetSessionConfigOptionRequest
	promptRequest             *acp.PromptRequest
	closedSessions            []acp.SessionId
	conn                      *acp.AgentConnection
}

func (f *fakeAgent) Initialize(ctx context.Context, params *acp.InitializeRequest) (*acp.InitializeResponse, error) {
	be.Equal(f.t, params.ProtocolVersion, acp.ProtocolVersion(1))
	be.True(f.t, params.ClientInfo != nil)
	be.Equal(f.t, params.ClientInfo.Name, "cpe-cli")
	return &acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion(1)}, nil
}

func (f *fakeAgent) NewSession(ctx context.Context, params *acp.NewSessionRequest) (*acp.NewSessionResponse, error) {
	be.Equal(f.t, params.Cwd, "/test/workspace")
	be.Equal(f.t, params.McpServers, []acp.McpServer{})

	configOptions := []acp.SessionConfigOption{
		acp.SelectSessionConfigOption(modelRefConfigID, "Model", "first-model", acp.SessionConfigSelectOptions{Ungrouped: &f.modelOptions}),
		acp.SelectSessionConfigOption(thinkingLevelConfigID, "Thinking level", "low", acp.SessionConfigSelectOptions{Ungrouped: &f.firstModelThinkingOptions}),
	}
	return &acp.NewSessionResponse{
		SessionID:     "test-session",
		ConfigOptions: &configOptions,
	}, nil
}

func (f *fakeAgent) SetSessionConfigOption(ctx context.Context, params *acp.SetSessionConfigOptionRequest) (*acp.SetSessionConfigOptionResponse, error) {
	f.setRequests = append(f.setRequests, *params)
	if params.ConfigID == modelRefConfigID {
		be.Equal(f.t, params.SessionID, acp.SessionId("test-session"))
		be.Equal(f.t, params.Value, any("second-model"))
		configOptions := []acp.SessionConfigOption{
			acp.SelectSessionConfigOption(modelRefConfigID, "Model", "second-model", acp.SessionConfigSelectOptions{Ungrouped: &f.modelOptions}),
		}
		if !f.setModelHasNoThinking {
			configOptions = append(configOptions, acp.SelectSessionConfigOption(thinkingLevelConfigID, "Thinking level", "medium", acp.SessionConfigSelectOptions{Ungrouped: &f.setModelThinkingOptions}))
		}
		return &acp.SetSessionConfigOptionResponse{ConfigOptions: configOptions}, nil
	}
	if params.ConfigID == thinkingLevelConfigID {
		be.Equal(f.t, params.SessionID, acp.SessionId("test-session"))
		be.Equal(f.t, params.Value, any("deep"))
		configOptions := []acp.SessionConfigOption{
			acp.SelectSessionConfigOption(modelRefConfigID, "Model", "second-model", acp.SessionConfigSelectOptions{Ungrouped: &f.modelOptions}),
			acp.SelectSessionConfigOption(thinkingLevelConfigID, "Thinking level", "deep", acp.SessionConfigSelectOptions{Ungrouped: &f.setModelThinkingOptions}),
		}
		return &acp.SetSessionConfigOptionResponse{ConfigOptions: configOptions}, nil
	}
	return nil, errors.New("unexpected config id")
}

func (f *fakeAgent) Prompt(ctx context.Context, params *acp.PromptRequest) (*acp.PromptResponse, error) {
	f.promptRequest = params
	be.Equal(f.t, params.SessionID, acp.SessionId("test-session"))
	be.Equal(f.t, params.Prompt, []acp.ContentBlock{acp.TextContentBlock("hello from cli")})
	if err := f.conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: params.SessionID,
		Update:    acp.AgentThoughtChunkSessionUpdate(acp.TextContentBlock("agent thought")),
	}); err != nil {
		return nil, err
	}
	if err := f.conn.SessionUpdate(ctx, &acp.SessionNotification{
		SessionID: params.SessionID,
		Update:    acp.AgentMessageChunkSessionUpdate(acp.TextContentBlock("agent answer")),
	}); err != nil {
		return nil, err
	}
	return &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (f *fakeAgent) CloseSession(ctx context.Context, params *acp.CloseSessionRequest) (*acp.CloseSessionResponse, error) {
	f.closedSessions = append(f.closedSessions, params.SessionID)
	return &acp.CloseSessionResponse{}, nil
}

func TestRun(t *testing.T) {
	t.Run("applies config, renders updates, and closes session", func(t *testing.T) {
		fake := &fakeAgent{
			t: t,
			modelOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "First", Value: "first-model"},
				{Name: "Second", Value: "second-model"},
			},
			firstModelThinkingOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "Low", Value: "low"},
				{Name: "High", Value: "high"},
			},
			setModelThinkingOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "Medium", Value: "medium"},
				{Name: "Deep", Value: "deep"},
			},
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := run(t.Context(), Options{
			Prompt:        "hello from cli",
			ModelRef:      "second-model",
			ThinkingLevel: "deep",
			Cwd:           "/test/workspace",
			Stdout:        &stdout,
			Stderr:        &stderr,
		}, func(ctx context.Context, transport acp.Transport, opts Options) error {
			return acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
				fake.conn = conn
				return fake
			})
		})
		be.Err(t, err, nil)

		be.Equal(t, len(fake.setRequests), 2)
		be.Equal(t, fake.setRequests[0].ConfigID, modelRefConfigID)
		be.Equal(t, fake.setRequests[0].Value, any("second-model"))
		be.Equal(t, fake.setRequests[1].ConfigID, thinkingLevelConfigID)
		be.Equal(t, fake.setRequests[1].Value, any("deep"))
		be.True(t, fake.promptRequest != nil)
		be.Equal(t, fake.closedSessions, []acp.SessionId{"test-session"})
		stdoutText := stdout.String()
		be.True(t, strings.Contains(stdoutText, "\n[thought] agent thought\n"))
		be.True(t, strings.Contains(stdoutText, "agent answer"))
		be.True(t, strings.Contains(stdoutText, "\n\n[stop: end_turn]\n"))
	})

	t.Run("rejects thinking for model without thinking values", func(t *testing.T) {
		fake := &fakeAgent{
			t: t,
			modelOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "First", Value: "first-model"},
				{Name: "Second", Value: "second-model"},
			},
			firstModelThinkingOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "Low", Value: "low"},
			},
			setModelHasNoThinking: true,
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := run(t.Context(), Options{
			Prompt:        "hello from cli",
			ModelRef:      "second-model",
			ThinkingLevel: "high",
			Cwd:           "/test/workspace",
			Stdout:        &stdout,
			Stderr:        &stderr,
		}, func(ctx context.Context, transport acp.Transport, opts Options) error {
			return acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
				fake.conn = conn
				return fake
			})
		})
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), `session config option "thinkingLevel" is not available`))
		be.Equal(t, len(fake.setRequests), 1)
		be.Equal(t, fake.setRequests[0].ConfigID, modelRefConfigID)
		be.Equal(t, fake.setRequests[0].Value, any("second-model"))
		be.True(t, fake.promptRequest == nil)
		be.Equal(t, fake.closedSessions, []acp.SessionId{"test-session"})
	})

	t.Run("rejects invalid model before prompt", func(t *testing.T) {
		fake := &fakeAgent{
			t: t,
			modelOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "First", Value: "first-model"},
			},
			firstModelThinkingOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "Low", Value: "low"},
			},
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := run(t.Context(), Options{
			Prompt:   "hello from cli",
			ModelRef: "missing-model",
			Cwd:      "/test/workspace",
			Stdout:   &stdout,
			Stderr:   &stderr,
		}, func(ctx context.Context, transport acp.Transport, opts Options) error {
			return acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
				fake.conn = conn
				return fake
			})
		})
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), `invalid modelRef "missing-model"; valid values: first-model`))
		be.Equal(t, len(fake.setRequests), 0)
		be.True(t, fake.promptRequest == nil)
		be.Equal(t, fake.closedSessions, []acp.SessionId{"test-session"})
	})

	t.Run("validates thinking against selected model", func(t *testing.T) {
		fake := &fakeAgent{
			t: t,
			modelOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "First", Value: "first-model"},
				{Name: "Second", Value: "second-model"},
			},
			firstModelThinkingOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "Low", Value: "low"},
			},
			setModelThinkingOptions: acp.UngroupedSessionConfigSelectOptions{
				{Name: "Medium", Value: "medium"},
				{Name: "Deep", Value: "deep"},
			},
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		err := run(t.Context(), Options{
			Prompt:        "hello from cli",
			ModelRef:      "second-model",
			ThinkingLevel: "high",
			Cwd:           "/test/workspace",
			Stdout:        &stdout,
			Stderr:        &stderr,
		}, func(ctx context.Context, transport acp.Transport, opts Options) error {
			return acp.RunAgent(ctx, transport, func(conn *acp.AgentConnection) any {
				fake.conn = conn
				return fake
			})
		})
		be.True(t, err != nil)
		be.True(t, strings.Contains(err.Error(), `invalid thinkingLevel "high"; valid values: medium, deep`))
		be.Equal(t, len(fake.setRequests), 1)
		be.Equal(t, fake.setRequests[0].ConfigID, modelRefConfigID)
		be.True(t, fake.promptRequest == nil)
		be.Equal(t, fake.closedSessions, []acp.SessionId{"test-session"})
	})
}
