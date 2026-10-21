package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/dyson"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/toolruntime"
)

func TestTurnCheckpoints(t *testing.T) {
	for _, test := range []struct {
		name        string
		code        string
		resultError bool
	}{
		{
			name: "successful result",
			code: "print('first result')",
		},
		{
			name:        "failed result",
			code:        "fail('first failed')",
			resultError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var history conversation.Store
				var id acp.SessionId
				started := make(chan struct{})
				release := make(chan struct{})
				prefix := "load('subprocess.star', 'subprocess')\nsubprocess.run(['checkpoint'])\n"
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Check: func(gai.GenerationRequest) error {
							record, err := history.Load(
								t.Context(),
								conversation.ID(id),
							)
							if err != nil {
								return err
							}
							if len(record.Dialog) != 1 || record.Dialog[0].Blocks[0].Content.String() != "do work" {
								return errors.New("user message was not saved before generation")
							}
							return nil
						},
						Response: compactionResponse(
							compactionCall(
								t,
								"first",
								"starlark",
								map[string]any{
									"code": prefix + test.code,
								},
							),
							compactionCall(
								t,
								"second",
								"starlark",
								map[string]any{
									"code": prefix + "print('second result')",
								},
							),
						),
					},
					agenttest.GenerateStep{
						Check: func(gai.GenerationRequest) error {
							record, err := history.Load(
								t.Context(),
								conversation.ID(id),
							)
							if err != nil {
								return err
							}
							if len(record.Dialog) != 4 || record.Dialog[3].Blocks[0].ID != "second" {
								return errors.New("tool results were not saved before following generation")
							}
							return nil
						},
						Response: compactionResponse(gai.TextBlock("done")),
					},
				)
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						history = deps.History
						deps.Hosts = func(ctx context.Context, config toolruntime.Config) (toolruntime.Host, error) {
							return toolruntime.New(
								ctx,
								config,
								dyson.StdlibConfig{
									FS: deps.Filesystem,
									CommandRunner: runnerFunc(func(ctx context.Context, _ dyson.Command) (dyson.CommandResult, error) {
										select {
										case started <- struct{}{}:
										case <-ctx.Done():
											return dyson.CommandResult{}, ctx.Err()
										}
										select {
										case <-release:
											return dyson.CommandResult{}, nil
										case <-ctx.Done():
											return dyson.CommandResult{}, ctx.Err()
										}
									}),
								},
							)
						}
					},
				)
				id = openFixtureSession(
					t,
					fixture.client,
				)
				finished := make(
					chan error,
					1,
				)
				go func() {
					_, err := fixture.client.Prompt(
						t.Context(),
						&acp.PromptRequest{
							SessionID: id,
							Prompt: []acp.ContentBlock{
								acp.TextContentBlock("do work"),
							},
						},
					)
					finished <- err
				}()
				for call := range 2 {
					select {
					case <-started:
					case err := <-finished:
						t.Fatalf(
							"turn ended before tool %d: %v",
							call,
							err,
						)
					case <-time.After(5 * time.Second):
						t.Fatal("tool did not start")
					}
					record, err := history.Load(
						t.Context(),
						conversation.ID(id),
					)
					if err != nil {
						t.Fatal(err)
					}
					if len(record.Dialog) != 2+call || len(record.Dialog[1].Blocks) != 2 || record.Dialog[1].ExtraFields["cpe.model"] != "one" {
						t.Fatalf(
							"tool intent was not saved intact: %#v",
							record.Dialog,
						)
					}
					if call == 1 {
						result := record.Dialog[2]
						if result.Role != gai.ToolResult || result.Blocks[0].ID != "first" || result.ToolResultError != test.resultError || !strings.Contains(
							result.Blocks[0].Content.String(),
							"first",
						) {
							t.Fatalf(
								"first result was not saved while second tool was running: %#v",
								result,
							)
						}
					}
					release <- struct{}{}
				}
				if err := <-finished; err != nil {
					t.Fatal(err)
				}
			},
		)
	}
}

func TestInterruptedCheckpointRestore(t *testing.T) {
	for _, operation := range []string{
		"load",
		"resume",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				call := func(id string) gai.Block {
					return compactionCall(
						t,
						id,
						"starlark",
						map[string]any{
							"code": "load('subprocess.star', 'subprocess')\nsubprocess.run(['historical'])",
						},
					)
				}
				record := conversation.Record{
					Info: conversation.Info{
						ID:  "interrupted",
						Cwd: "/work",
					},
					Options: models.Selection{
						Model: "one",
					},
					Dialog: gai.Dialog{
						{
							Role: gai.User,
							Blocks: []gai.Block{
								gai.TextBlock("old task"),
							},
						},
						{
							Role: gai.Assistant,
							Blocks: []gai.Block{
								call("finished"),
								call("unfinished"),
							},
							ExtraFields: map[string]any{
								"cpe.model": "one",
							},
						},
						gai.ToolResultMessage(
							"finished",
							gai.TextBlock("durable result"),
						),
					},
				}
				var executions atomic.Int32
				generator := agenttest.NewScriptedGenerator(agenttest.GenerateStep{
					Check: func(request gai.GenerationRequest) error {
						result := request.Dialog[3]
						if result.Role != gai.ToolResult || !result.ToolResultError || result.Blocks[0].ID != "unfinished" || !strings.Contains(
							result.Blocks[0].Content.String(),
							"outcome uncertain",
						) {
							return errors.New("unfinished call was not marked uncertain in model context")
						}
						return nil
					},
					Response: compactionResponse(gai.TextBlock("inspect state before retrying")),
				})
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						if err := deps.History.Save(
							t.Context(),
							record,
						); err != nil {
							t.Fatal(err)
						}
						deps.Hosts = func(ctx context.Context, config toolruntime.Config) (toolruntime.Host, error) {
							return toolruntime.New(
								ctx,
								config,
								dyson.StdlibConfig{
									FS: deps.Filesystem,
									CommandRunner: runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
										executions.Add(1)
										return dyson.CommandResult{}, nil
									}),
								},
							)
						}
					},
				)
				var err error
				if operation == "load" {
					_, err = fixture.client.LoadSession(
						t.Context(),
						&acp.LoadSessionRequest{
							SessionID: "interrupted",
							Cwd:       "/work",
						},
					)
				} else {
					_, err = fixture.client.ResumeSession(
						t.Context(),
						&acp.ResumeSessionRequest{
							SessionID: "interrupted",
							Cwd:       "/work",
						},
					)
				}
				if err != nil {
					t.Fatal(err)
				}
				restored, err := fixture.store.Load(
					t.Context(),
					record.Info.ID,
				)
				if err != nil {
					t.Fatal(err)
				}
				if len(restored.Dialog) != 4 || restored.Dialog[2].ToolResultError || restored.Dialog[2].Blocks[0].Content.String() != "durable result" || !restored.Dialog[3].ToolResultError {
					t.Fatalf(
						"restoration did not preserve results and settle unfinished calls: %#v",
						restored.Dialog,
					)
				}
				promptFixture(
					t,
					fixture.client,
					"interrupted",
					"continue",
				)
				if executions.Load() != 0 {
					t.Fatal("restoration executed a historical tool call")
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				if operation == "load" {
					fixture.notifications.mu.Lock()
					defer fixture.notifications.mu.Unlock()
					found := false
					for _, update := range fixture.notifications.updates {
						if update.Update.ToolCallID == "unfinished" && update.Update.Status != nil && *update.Update.Status == acp.ToolCallStatusFailed {
							found = true
						}
					}
					if !found {
						t.Fatal("load did not report the interrupted call to ACP")
					}
				}
			},
		)
	}
}

type failingCheckpointStore struct {
	conversation.Store
	messages int
	once     bool
	failures atomic.Int32
}

func (s *failingCheckpointStore) Save(ctx context.Context, record conversation.Record) error {
	if len(record.Dialog) >= s.messages && (!s.once || s.failures.Load() == 0) {
		s.failures.Add(1)
		return errors.New("progress checkpoint unavailable")
	}
	return s.Store.Save(
		ctx,
		record,
	)
}

func TestTurnCheckpointFailures(t *testing.T) {
	for _, test := range []struct {
		name            string
		messages        int
		once            bool
		wantGenerations int
		wantExecutions  int32
		wantSaved       int
	}{
		{
			name:      "user checkpoint prevents generation",
			messages:  1,
			wantSaved: 0,
		},
		{
			name:            "intent checkpoint prevents execution",
			messages:        2,
			wantGenerations: 1,
			wantSaved:       1,
		},
		{
			name:            "result checkpoint prevents later tools and generation",
			messages:        3,
			wantGenerations: 1,
			wantExecutions:  1,
			wantSaved:       2,
		},
		{
			name:            "cleanup retains completed result after a transient save failure",
			messages:        3,
			once:            true,
			wantGenerations: 1,
			wantExecutions:  1,
			wantSaved:       4,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var executions atomic.Int32
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Response: compactionResponse(
							compactionCall(
								t,
								"first",
								"starlark",
								map[string]any{
									"code": "load('subprocess.star', 'subprocess')\nsubprocess.run(['effect'])\nprint('completed')",
								},
							),
							compactionCall(
								t,
								"second",
								"starlark",
								map[string]any{
									"code": "load('subprocess.star', 'subprocess')\nsubprocess.run(['effect'])",
								},
							),
						),
					},
					agenttest.GenerateStep{
						Response: compactionResponse(gai.TextBlock("done")),
					},
				)
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.History = &failingCheckpointStore{
							Store:    deps.History,
							messages: test.messages,
							once:     test.once,
						}
						deps.Hosts = func(ctx context.Context, config toolruntime.Config) (toolruntime.Host, error) {
							return toolruntime.New(
								ctx,
								config,
								dyson.StdlibConfig{
									FS: deps.Filesystem,
									CommandRunner: runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
										executions.Add(1)
										return dyson.CommandResult{}, nil
									}),
								},
							)
						}
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				_, err := fixture.client.Prompt(
					t.Context(),
					&acp.PromptRequest{
						SessionID: id,
						Prompt: []acp.ContentBlock{
							acp.TextContentBlock("do work"),
						},
					},
				)
				if err == nil || !strings.Contains(
					err.Error(),
					"progress checkpoint unavailable",
				) {
					t.Fatalf(
						"checkpoint error not reported: %v",
						err,
					)
				}
				if len(generator.Requests()) != test.wantGenerations || executions.Load() != test.wantExecutions {
					t.Fatalf(
						"continued after failed checkpoint: %d generations, %d executions",
						len(generator.Requests()),
						executions.Load(),
					)
				}
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil {
					t.Fatal(err)
				}
				if len(record.Dialog) != test.wantSaved {
					t.Fatalf(
						"saved %d messages, want %d",
						len(record.Dialog),
						test.wantSaved,
					)
				}
				if test.once {
					if record.Dialog[2].ToolResultError || record.Dialog[2].Blocks[0].ID != "first" || !strings.Contains(
						record.Dialog[2].Blocks[0].Content.String(),
						"completed",
					) || !record.Dialog[3].ToolResultError || record.Dialog[3].Blocks[0].ID != "second" {
						t.Fatal("cleanup lost a completed result or left the unfinished call unsettled")
					}
				}
			},
		)
	}
}
