package agent_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/nalgeon/be"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

type agentLoopScenario struct {
	name           string
	cfg            config.Config
	initialDialog  gai.Dialog
	script         []agentLoopScriptedGeneration
	newTools       func(t *testing.T) []agentLoopToolFixture
	newDialogSaver func(t *testing.T) (*storage.Sqlite, storage.DialogSaver)
	check          func(t *testing.T, result agentLoopScenarioResult)
}

type agentLoopScriptedGeneration struct {
	response gai.Response
	err      error
}

type agentLoopScenarioResult struct {
	dialog       gai.Dialog
	err          error
	model        *agentLoopScriptedModel
	toolRecorder *agentLoopToolRecorder
	store        *storage.Sqlite
	stdout       *bytes.Buffer
	stderr       *bytes.Buffer
}

type agentLoopScriptedModel struct {
	script []agentLoopScriptedGeneration
	calls  []agentLoopModelCall
	tools  []gai.Tool
}

type agentLoopModelCall struct {
	dialog gai.Dialog
	opts   *gai.GenOpts
}

func (m *agentLoopScriptedModel) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	_ = ctx
	m.calls = append(m.calls, agentLoopModelCall{
		dialog: cloneDialogForAgentLoopTest(dialog),
		opts:   cloneGenOptsForAgentLoopTest(opts),
	})
	if len(m.script) == 0 {
		return gai.Response{}, errors.New("no scripted generation response")
	}
	step := m.script[0]
	m.script = m.script[1:]
	return step.response, step.err
}

func (m *agentLoopScriptedModel) Register(tool gai.Tool) error {
	m.tools = append(m.tools, tool)
	return nil
}

type agentLoopToolFixture struct {
	tool     gai.Tool
	callback gai.ToolCallback
}

type agentLoopToolRecorder struct {
	calls []agentLoopToolInvocation
}

type agentLoopToolInvocation struct {
	name       string
	id         string
	paramsJSON string
}

type recordingToolCallback struct {
	name        string
	resultTexts []string
	err         error
	onCall      func(ctx context.Context, params json.RawMessage, id string, index int) (gai.Message, error)
	calls       int
	recorder    *agentLoopToolRecorder
}

func newRecordingToolCallback(resultTexts ...string) *recordingToolCallback {
	return &recordingToolCallback{resultTexts: resultTexts}
}

func (c *recordingToolCallback) Call(ctx context.Context, params json.RawMessage, id string) (gai.Message, error) {
	index := c.calls
	c.calls++
	if c.recorder != nil {
		c.recorder.calls = append(c.recorder.calls, agentLoopToolInvocation{
			name:       c.name,
			id:         id,
			paramsJSON: string(params),
		})
	}
	if c.onCall != nil {
		return c.onCall(ctx, params, id, index)
	}
	if c.err != nil {
		return gai.Message{}, c.err
	}
	if index >= len(c.resultTexts) {
		return gai.ToolResultMessage(id, gai.TextBlock("")), nil
	}
	return gai.ToolResultMessage(id, gai.TextBlock(c.resultTexts[index])), nil
}

func assistantText(text string) agentLoopScriptedGeneration {
	return agentLoopScriptedGeneration{
		response: gai.Response{
			FinishReason: gai.EndTurn,
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(text)}},
			},
		},
	}
}

func assistantToolUse(blocks ...gai.Block) agentLoopScriptedGeneration {
	return agentLoopScriptedGeneration{
		response: gai.Response{
			FinishReason: gai.ToolUse,
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: blocks},
			},
		},
	}
}

func mustToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, name, params)
	be.Err(t, err, nil)
	return block
}

// TODO: Extend this behavior scaffold with scenarios for callback errors,
// model errors before and after tool results, continuation/fork persistence,
// compaction warning injection, compaction branch restart, max compaction
// limits, direct GenOpts handling, and Responses prompt-cache/reasoning opts.
func TestAgentLoopScenarios(t *testing.T) {
	tests := []agentLoopScenario{
		{
			name: "single assistant response persists dialog",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}},
			},
			script: []agentLoopScriptedGeneration{
				{
					response: gai.Response{
						FinishReason: gai.EndTurn,
						Candidates: []gai.Message{
							{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("hi there")}},
						},
					},
				},
			},
			newDialogSaver: newSQLiteAgentLoopDialogSaver,
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, nil)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant)
				requireMessageText(t, result.dialog[0], "hello")
				requireMessageText(t, result.dialog[1], "hi there")
				requirePersistedMessages(t, result.store, []persistedMessageExpectation{
					{role: gai.User, text: "hello"},
					{role: gai.Assistant, text: "hi there"},
				})
				be.Equal(t, result.stdout.String(), "hi there")
				be.True(t, strings.Contains(result.stderr.String(), "> message_id: `"))
				be.Equal(t, len(result.model.calls), 1)
				requireDialogRoles(t, result.model.calls[0].dialog, gai.User)
			},
		},
		{
			name: "single assistant response in incognito mode does not persist",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}},
			},
			script: []agentLoopScriptedGeneration{
				{
					response: gai.Response{
						FinishReason: gai.EndTurn,
						Candidates: []gai.Message{
							{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("hi there")}},
						},
					},
				},
			},
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, nil)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant)
				requireMessageText(t, result.dialog[0], "hello")
				requireMessageText(t, result.dialog[1], "hi there")
				be.True(t, result.store == nil)
				be.Equal(t, result.stdout.String(), "hi there")
				be.Equal(t, result.stderr.String(), "")
				be.Equal(t, len(result.model.calls), 1)
				requireDialogRoles(t, result.model.calls[0].dialog, gai.User)
			},
		},
		{
			name: "tool callback executes and continues",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use lookup")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"})),
				assistantText("final answer"),
			},
			newTools: func(t *testing.T) []agentLoopToolFixture {
				t.Helper()
				return []agentLoopToolFixture{{
					tool:     gai.Tool{Name: "lookup"},
					callback: newRecordingToolCallback("lookup result"),
				}}
			},
			newDialogSaver: newSQLiteAgentLoopDialogSaver,
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, nil)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant, gai.ToolResult, gai.Assistant)
				requireMessageText(t, result.dialog[2], "lookup result")
				requireMessageText(t, result.dialog[3], "final answer")
				requirePersistedRoles(t, result.store, gai.User, gai.Assistant, gai.ToolResult, gai.Assistant)
				requireToolInvocations(t, result.toolRecorder, []agentLoopToolInvocation{{
					name:       "lookup",
					id:         "call_1",
					paramsJSON: `{"q":"docs"}`,
				}})
				be.Equal(t, result.stdout.String(), "final answer")
				be.True(t, strings.Contains(result.stderr.String(), `Tool "lookup" result`))
				be.Equal(t, len(result.model.calls), 2)
				requireDialogRoles(t, result.model.calls[1].dialog, gai.User, gai.Assistant, gai.ToolResult)
			},
		},
		{
			name: "multiple tool callbacks execute in order",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use tools")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(
					mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": "docs"}),
					mustToolCallBlock(t, "call_2", "read", map[string]any{"path": "README.md"}),
				),
				assistantText("combined answer"),
			},
			newTools: func(t *testing.T) []agentLoopToolFixture {
				t.Helper()
				return []agentLoopToolFixture{
					{tool: gai.Tool{Name: "lookup"}, callback: newRecordingToolCallback("lookup result")},
					{tool: gai.Tool{Name: "read"}, callback: newRecordingToolCallback("read result")},
				}
			},
			newDialogSaver: newSQLiteAgentLoopDialogSaver,
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, nil)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant, gai.ToolResult, gai.ToolResult, gai.Assistant)
				requireMessageText(t, result.dialog[2], "lookup result")
				requireMessageText(t, result.dialog[3], "read result")
				requireMessageText(t, result.dialog[4], "combined answer")
				requirePersistedRoles(t, result.store, gai.User, gai.Assistant, gai.ToolResult, gai.ToolResult, gai.Assistant)
				requireToolInvocations(t, result.toolRecorder, []agentLoopToolInvocation{
					{name: "lookup", id: "call_1", paramsJSON: `{"q":"docs"}`},
					{name: "read", id: "call_2", paramsJSON: `{"path":"README.md"}`},
				})
				be.Equal(t, result.stdout.String(), "combined answer")
				be.Equal(t, len(result.model.calls), 2)
				requireDialogRoles(t, result.model.calls[1].dialog, gai.User, gai.Assistant, gai.ToolResult, gai.ToolResult)
			},
		},
		{
			name: "unknown tool returns an error after persisting tool call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("use missing tool")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "missing", map[string]any{})),
			},
			newDialogSaver: newSQLiteAgentLoopDialogSaver,
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, `tool 'missing' not found`)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant)
				requirePersistedRoles(t, result.store, gai.User, gai.Assistant)
				be.Equal(t, result.stdout.String(), "")
				be.True(t, strings.Contains(result.stderr.String(), "missing"))
				be.Equal(t, len(result.model.calls), 1)
			},
		},
		{
			name: "nil callback terminates after tool call",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("finish")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "finish", map[string]any{})),
			},
			newTools: func(t *testing.T) []agentLoopToolFixture {
				t.Helper()
				return []agentLoopToolFixture{{tool: gai.Tool{Name: "finish"}, callback: nil}}
			},
			newDialogSaver: newSQLiteAgentLoopDialogSaver,
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, nil)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant)
				requirePersistedRoles(t, result.store, gai.User, gai.Assistant)
				requireToolInvocations(t, result.toolRecorder, nil)
				be.Equal(t, result.stdout.String(), "")
				be.True(t, strings.Contains(result.stderr.String(), "finish"))
				be.Equal(t, len(result.model.calls), 1)
			},
		},
		{
			name: "invalid tool parameters are returned as tool result",
			initialDialog: gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("lookup with bad params")}},
			},
			script: []agentLoopScriptedGeneration{
				assistantToolUse(mustToolCallBlock(t, "call_1", "lookup", map[string]any{"q": 123})),
				assistantText("asked for correction"),
			},
			newTools: func(t *testing.T) []agentLoopToolFixture {
				t.Helper()
				callback := newRecordingToolCallback()
				callback.onCall = func(ctx context.Context, params json.RawMessage, id string, index int) (gai.Message, error) {
					_ = ctx
					_ = index
					var input struct {
						Q string `json:"q"`
					}
					if err := json.Unmarshal(params, &input); err != nil || input.Q == "" {
						return gai.ToolResultMessage(id, gai.TextBlock("invalid parameters")), nil
					}
					return gai.ToolResultMessage(id, gai.TextBlock("lookup result")), nil
				}
				return []agentLoopToolFixture{{
					tool:     gai.Tool{Name: "lookup"},
					callback: callback,
				}}
			},
			newDialogSaver: newSQLiteAgentLoopDialogSaver,
			check: func(t *testing.T, result agentLoopScenarioResult) {
				t.Helper()
				be.Err(t, result.err, nil)
				requireDialogRoles(t, result.dialog, gai.User, gai.Assistant, gai.ToolResult, gai.Assistant)
				requireMessageText(t, result.dialog[2], "invalid parameters")
				requireMessageText(t, result.dialog[3], "asked for correction")
				requirePersistedRoles(t, result.store, gai.User, gai.Assistant, gai.ToolResult, gai.Assistant)
				requireToolInvocations(t, result.toolRecorder, []agentLoopToolInvocation{{
					name:       "lookup",
					id:         "call_1",
					paramsJSON: `{"q":123}`,
				}})
				be.Equal(t, result.stdout.String(), "asked for correction")
				be.True(t, strings.Contains(result.stderr.String(), "invalid parameters"))
				be.Equal(t, len(result.model.calls), 2)
				requireDialogRoles(t, result.model.calls[1].dialog, gai.User, gai.Assistant, gai.ToolResult)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &agentLoopScriptedModel{script: slices.Clone(tt.script)}
			var store *storage.Sqlite
			var saver storage.DialogSaver
			if tt.newDialogSaver != nil {
				store, saver = tt.newDialogSaver(t)
			}
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			loop := agent.NewRuntime(model, tt.cfg, saver, stdout, stderr, false)
			toolRecorder := &agentLoopToolRecorder{}
			if tt.newTools != nil {
				for _, fixture := range tt.newTools(t) {
					if callback, ok := fixture.callback.(*recordingToolCallback); ok {
						callback.name = fixture.tool.Name
						callback.recorder = toolRecorder
					}
					be.Err(t, loop.Register(fixture.tool, fixture.callback), nil)
				}
			}
			dialog, err := loop.Generate(context.Background(), cloneDialogForAgentLoopTest(tt.initialDialog), nil)

			result := agentLoopScenarioResult{
				dialog:       dialog,
				err:          err,
				model:        model,
				toolRecorder: toolRecorder,
				store:        store,
				stdout:       stdout,
				stderr:       stderr,
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func newSQLiteAgentLoopDialogSaver(t *testing.T) (*storage.Sqlite, storage.DialogSaver) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	be.Err(t, err, nil)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store, err := storage.NewSqlite(context.Background(), db)
	be.Err(t, err, nil)
	return store, store
}

func cloneDialogForAgentLoopTest(dialog gai.Dialog) gai.Dialog {
	clone := make(gai.Dialog, len(dialog))
	for i, msg := range dialog {
		clone[i] = msg
		clone[i].Blocks = slices.Clone(msg.Blocks)
		clone[i].ExtraFields = maps.Clone(msg.ExtraFields)
		for j := range clone[i].Blocks {
			clone[i].Blocks[j].ExtraFields = maps.Clone(clone[i].Blocks[j].ExtraFields)
		}
	}
	return clone
}

func cloneGenOptsForAgentLoopTest(opts *gai.GenOpts) *gai.GenOpts {
	if opts == nil {
		return nil
	}
	clone := *opts
	clone.StopSequences = slices.Clone(opts.StopSequences)
	clone.OutputModalities = slices.Clone(opts.OutputModalities)
	clone.ExtraArgs = maps.Clone(opts.ExtraArgs)
	return &clone
}

func requireDialogRoles(t *testing.T, dialog gai.Dialog, roles ...gai.Role) {
	t.Helper()
	be.Equal(t, len(dialog), len(roles))
	if len(dialog) != len(roles) {
		return
	}
	for i, role := range roles {
		be.Equal(t, dialog[i].Role, role)
	}
}

func requireMessageText(t *testing.T, msg gai.Message, want string) {
	t.Helper()
	be.Equal(t, len(msg.Blocks), 1)
	if len(msg.Blocks) != 1 {
		return
	}
	be.Equal(t, msg.Blocks[0].ModalityType, gai.Text)
	be.Equal(t, msg.Blocks[0].Content.String(), want)
}

type persistedMessageExpectation struct {
	role gai.Role
	text string
}

func requirePersistedMessages(t *testing.T, store *storage.Sqlite, want []persistedMessageExpectation) {
	t.Helper()
	got := persistedMessages(t, store)
	be.Equal(t, len(got), len(want))
	if len(got) != len(want) {
		return
	}
	for i, expectation := range want {
		be.Equal(t, got[i].Role, expectation.role)
		requireMessageText(t, got[i], expectation.text)
	}
}

func requirePersistedRoles(t *testing.T, store *storage.Sqlite, roles ...gai.Role) {
	t.Helper()
	got := persistedMessages(t, store)
	be.Equal(t, len(got), len(roles))
	if len(got) != len(roles) {
		return
	}
	for i, role := range roles {
		be.Equal(t, got[i].Role, role)
	}
}

func persistedMessages(t *testing.T, store *storage.Sqlite) []gai.Message {
	t.Helper()
	be.True(t, store != nil)
	if store == nil {
		return nil
	}

	msgs, err := store.ListMessages(context.Background(), storage.ListMessagesOptions{AscendingOrder: true})
	be.Err(t, err, nil)

	var got []gai.Message
	for msg := range msgs {
		got = append(got, msg)
	}
	return got
}

func requireToolInvocations(t *testing.T, recorder *agentLoopToolRecorder, want []agentLoopToolInvocation) {
	t.Helper()
	be.True(t, recorder != nil)
	if recorder == nil {
		return
	}
	be.Equal(t, len(recorder.calls), len(want))
	if len(recorder.calls) != len(want) {
		return
	}
	for i, invocation := range want {
		be.Equal(t, recorder.calls[i], invocation)
	}
}
