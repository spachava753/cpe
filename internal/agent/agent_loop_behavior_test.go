package agent_test

import (
	"bytes"
	"context"
	"database/sql"
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
	newDialogSaver func(t *testing.T) (*storage.Sqlite, storage.DialogSaver)
	check          func(t *testing.T, result agentLoopScenarioResult)
}

type agentLoopScriptedGeneration struct {
	response gai.Response
	err      error
}

type agentLoopScenarioResult struct {
	dialog gai.Dialog
	err    error
	model  *agentLoopScriptedModel
	store  *storage.Sqlite
	stdout *bytes.Buffer
	stderr *bytes.Buffer
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
			dialog, err := loop.Generate(context.Background(), cloneDialogForAgentLoopTest(tt.initialDialog), nil)

			result := agentLoopScenarioResult{
				dialog: dialog,
				err:    err,
				model:  model,
				store:  store,
				stdout: stdout,
				stderr: stderr,
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
	for i, role := range roles {
		be.Equal(t, dialog[i].Role, role)
	}
}

func requireMessageText(t *testing.T, msg gai.Message, want string) {
	t.Helper()
	be.Equal(t, len(msg.Blocks), 1)
	be.Equal(t, msg.Blocks[0].ModalityType, gai.Text)
	be.Equal(t, msg.Blocks[0].Content.String(), want)
}

type persistedMessageExpectation struct {
	role gai.Role
	text string
}

func requirePersistedMessages(t *testing.T, store *storage.Sqlite, want []persistedMessageExpectation) {
	t.Helper()
	be.True(t, store != nil)

	msgs, err := store.ListMessages(context.Background(), storage.ListMessagesOptions{AscendingOrder: true})
	be.Err(t, err, nil)

	var got []gai.Message
	for msg := range msgs {
		got = append(got, msg)
	}
	be.Equal(t, len(got), len(want))
	for i, expectation := range want {
		be.Equal(t, got[i].Role, expectation.role)
		requireMessageText(t, got[i], expectation.text)
	}
}
