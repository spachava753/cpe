package agent

import (
	"context"
	"errors"
	"fmt"
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

func compactionCall(t *testing.T, id, name string, arguments map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(
		id,
		name,
		arguments,
	)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func compactionResponse(blocks ...gai.Block) gai.Response {
	reason := gai.EndTurn
	for _, block := range blocks {
		if block.BlockType == gai.ToolCall {
			reason = gai.ToolUse
		}
	}
	return gai.Response{
		FinishReason: reason,
		Candidates: []gai.Message{
			{
				Role:   gai.Assistant,
				Blocks: blocks,
			},
		},
	}
}

func TestCompaction(t *testing.T) {
	for _, operation := range []string{
		"next turn",
		"model switch",
		"fork",
		"load",
		"resume",
		"repeat compaction",
		"compact after resume",
		"script failure after compact",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				const summary = "Goal: update CPE. Next: finish the requested work."
				setup := compactionCall(
					t,
					"setup",
					"starlark",
					map[string]any{
						"code": "state['counter'] = 1",
					},
				)
				code := "save_summary = compact\nsave_summary('initial draft')\nparts = ['Goal: update CPE.', 'Next: finish the requested work.']\nresult = save_summary(summary=' '.join(parts))\nif result != None:\n    fail('compact must return None')"
				if operation == "script failure after compact" {
					code += "\nfail('later failure')"
				}
				compact := compactionCall(
					t,
					"compact-first",
					"starlark",
					map[string]any{
						"code": code,
					},
				)
				increment := compactionCall(
					t,
					"increment",
					"starlark",
					map[string]any{
						"code": "state['counter'] += 1\nprint(state['counter'])",
					},
				)
				read := compactionCall(
					t,
					"read",
					"starlark",
					map[string]any{
						"code": "print(state['counter'])",
					},
				)
				if operation == "repeat compaction" || operation == "compact after resume" {
					code := "compact('Updated goal and next steps.')"
					read = compactionCall(
						t,
						"compact-second",
						"starlark",
						map[string]any{
							"code": code,
						},
					)
				}
				var fixture agentFixture
				var id acp.SessionId
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if request.Instructions.Role != gai.System || !strings.Contains(
								request.Instructions.Blocks[0].Content.String(),
								"summary-writing guidance",
							) {
								return errors.New("guidance is missing from the initial system instructions")
							}
							return nil
						},
						Response: compactionResponse(setup),
					},
					agenttest.GenerateStep{
						Response: compactionResponse(gai.TextBlock("old answer")),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if request.Dialog[len(request.Dialog)-1].Blocks[0].Content.String() != "Compact the conversation now by calling compact(summary) in starlark." {
								return errors.New("slash command did not request compaction")
							}
							return nil
						},
						Response: compactionResponse(
							compact,
							increment,
						),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if len(request.Dialog) != 1 || request.Dialog[0].Role != gai.User || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\n"+summary {
								return fmt.Errorf(
									"next context must contain only the final scripted summary: %#v",
									request.Dialog,
								)
							}
							persisted, err := fixture.store.Load(
								context.Background(),
								conversation.ID(id),
							)
							if err != nil || persisted.Compaction == nil || persisted.Compaction.Summary != summary || persisted.Compaction.AfterMessages != 8 || len(persisted.Dialog) != 8 || persisted.Dialog[0].Blocks[0].Content.String() != "old task detail" || persisted.Dialog[7].Blocks[0].Content.String() != "2\n" || persisted.Dialog[6].ToolResultError != (operation == "script failure after compact") {
								return fmt.Errorf(
									"summary and full transcript not saved before next generation: %#v, %v",
									persisted,
									err,
								)
							}
							return nil
						},
						Response: compactionResponse(gai.TextBlock("compacted")),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							wantMessages := 3
							switch operation {
							case "load", "resume", "fork", "compact after resume":
								wantMessages++
								if request.Dialog[2].Blocks[0].Content.String() != runtimeResetNotice {
									return errors.New("restored summary is missing its runtime reset boundary")
								}
							}
							if len(request.Dialog) != wantMessages || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\n"+summary {
								return errors.New("later turn lost compacted context")
							}
							if operation == "model switch" && request.Model != "two" {
								return errors.New("model switch was not retained")
							}
							return nil
						},
						Response: compactionResponse(read),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							last := request.Dialog[len(request.Dialog)-1]
							switch operation {
							case "repeat compaction", "compact after resume":
								if len(request.Dialog) != 1 || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\nUpdated goal and next steps." {
									return errors.New("second compaction did not replace the earlier working context")
								}
							case "load", "resume", "fork":
								if !last.ToolResultError {
									return errors.New("restoration replayed historical runtime effects")
								}
							default:
								if last.Blocks[0].Content.String() != "2\n" {
									return errors.New("compaction reset or repeated runtime effects")
								}
							}
							return nil
						},
						Response: compactionResponse(gai.TextBlock("done")),
					},
				)
				fixture = serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(config *Config, _ *Dependencies, _ *acp.InitializeRequest) {
						config.Instructions += "\n\nsummary-writing guidance"
					},
				)
				id = openFixtureSession(
					t,
					fixture.client,
				)
				promptFixture(
					t,
					fixture.client,
					id,
					"old task detail",
				)
				promptFixture(
					t,
					fixture.client,
					id,
					"/compact",
				)
				target := id
				switch operation {
				case "model switch":
					_, err := fixture.client.SetSessionConfigOption(
						t.Context(),
						&acp.SetSessionConfigOptionRequest{
							SessionID: id,
							ConfigID:  "model",
							Value:     "two",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
				case "fork":
					fork, err := fixture.client.ForkSession(
						t.Context(),
						&acp.ForkSessionRequest{
							SessionID: id,
							Cwd:       "/work",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					target = fork.SessionID
				case "load", "resume", "compact after resume":
					_, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: id,
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					if operation == "load" {
						_, err = fixture.client.LoadSession(
							t.Context(),
							&acp.LoadSessionRequest{
								SessionID: id,
								Cwd:       "/work",
							},
						)
					} else {
						_, err = fixture.client.ResumeSession(
							t.Context(),
							&acp.ResumeSessionRequest{
								SessionID: id,
								Cwd:       "/work",
							},
						)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				promptFixture(
					t,
					fixture.client,
					target,
					"continue",
				)
				if len(generator.Requests()) != 6 {
					t.Fatal("unexpected model calls")
				}
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(target),
				)
				if err != nil || len(record.Dialog) != 13 || record.Dialog[0].Blocks[0].Content.String() != "old task detail" || record.Compaction == nil {
					t.Fatalf(
						"full transcript lost: %#v, %v",
						record,
						err,
					)
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				defer fixture.notifications.mu.Unlock()
				foundCommand := false
				foundReplay := false
				for _, notification := range fixture.notifications.updates {
					for _, command := range notification.Update.AvailableCommands {
						foundCommand = foundCommand || command.Name == "compact"
					}
					if notification.Update.SessionUpdate == acp.SessionUpdateTypeUserMessageChunk {
						content, ok := notification.Update.Content.(acp.ContentBlock)
						foundReplay = foundReplay || ok && content.Text == "old task detail"
					}
				}
				if !foundCommand || operation == "load" && !foundReplay {
					t.Fatal("command discovery or full transcript replay failed")
				}
			},
		)
	}
}

func TestCompactionWithSubprocess(t *testing.T) {
	for _, order := range []string{
		"command before compaction",
		"command after compaction",
	} {
		t.Run(
			order,
			func(t *testing.T) {
				script := compactionCall(
					t,
					"script",
					"starlark",
					map[string]any{
						"code": "load('subprocess.star', 'subprocess')\nresult = subprocess.run(['capture'], capture_output=True)\ncompact(summary='Result: ' + result.stdout.decode())\nprint('script output after compact')",
					},
				)
				command := compactionCall(
					t,
					"command",
					"starlark",
					map[string]any{
						"code": "load('subprocess.star', 'subprocess')\nr = subprocess.run(['effect'], capture_output=True)\nprint(r.stdout.decode())",
					},
				)
				calls := []gai.Block{
					script,
					command,
					gai.TextBlock("assistant text in the compacted response"),
				}
				if order == "command before compaction" {
					calls[0], calls[1] = calls[1], calls[0]
				}
				var effects atomic.Int32
				var fixture agentFixture
				var id acp.SessionId
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Response: compactionResponse(calls...),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if effects.Load() != 2 {
								return errors.New("generation resumed before all command effects finished")
							}
							if len(request.Dialog) != 1 || request.Dialog[0].Role != gai.User || len(request.Dialog[0].Blocks) != 1 || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\nResult: captured" {
								return errors.New("working context must contain only the summary built from command output")
							}
							record, err := fixture.store.Load(
								context.Background(),
								conversation.ID(id),
							)
							if err != nil {
								return err
							}
							if record.Compaction == nil || record.Compaction.AfterMessages != 4 || len(record.Dialog) != 4 {
								return errors.New("full completed response was not checkpointed before generation")
							}
							var saved strings.Builder
							for _, message := range record.Dialog {
								if message.ToolResultError {
									return errors.New("a command failed instead of finishing normally")
								}
								for _, block := range message.Blocks {
									saved.WriteString(block.Content.String())
								}
							}
							for _, text := range []string{
								"independent command output",
								"script output after compact",
								"assistant text in the compacted response",
							} {
								if !strings.Contains(
									saved.String(),
									text,
								) {
									return fmt.Errorf(
										"full transcript lost %q",
										text,
									)
								}
							}
							return nil
						},
						Response: compactionResponse(gai.TextBlock("done")),
					},
				)
				fixture = serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.Hosts = func(ctx context.Context, config toolruntime.Config) (toolruntime.Host, error) {
							return toolruntime.New(
								ctx,
								config,
								dyson.StdlibConfig{
									FS: deps.Filesystem,
									CommandRunner: runnerFunc(func(_ context.Context, command dyson.Command) (dyson.CommandResult, error) {
										effects.Add(1)
										output := "captured"
										if command.Args[0] == "effect" {
											output = "independent command output"
										}
										return dyson.CommandResult{
											Stdout: []byte(output),
										}, nil
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
				promptFixture(
					t,
					fixture.client,
					id,
					"run the commands and compact",
				)
				if len(generator.Requests()) != 2 || effects.Load() != 2 {
					t.Fatal("compaction repeated model requests or command effects")
				}
			},
		)
	}
}

// Reject only compaction checkpoints, allowing session setup and older history to persist.
type rejectCompactionStore struct {
	conversation.Store
}

func (s rejectCompactionStore) Save(ctx context.Context, record conversation.Record) error {
	if record.Compaction != nil {
		return errors.New("compaction checkpoint unavailable")
	}
	return s.Store.Save(
		ctx,
		record,
	)
}

func TestCompactionFailures(t *testing.T) {
	for _, test := range []struct {
		name              string
		code              string
		checkpointFailure bool
		cancelBatch       bool
	}{
		{
			name: "empty summary",
			code: "compact('')",
		},
		{
			name: "whitespace summary",
			code: "compact(' \\n\\t')",
		},
		{
			name: "wrong type",
			code: "compact(1)",
		},
		{
			name: "missing summary",
			code: "compact()",
		},
		{
			name: "extra argument",
			code: "compact('Saved goal.', extra=True)",
		},
		{
			name:              "checkpoint failure prevents following generation",
			code:              "compact('Saved goal.')",
			checkpointFailure: true,
		},
		{
			name:        "cancellation in a later tool preserves accepted summary",
			code:        "compact('Saved goal.')",
			cancelBatch: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				calls := []gai.Block{
					compactionCall(
						t,
						"compact",
						"starlark",
						map[string]any{
							"code": test.code,
						},
					),
				}
				if test.cancelBatch {
					calls = append(
						calls,
						compactionCall(
							t,
							"block",
							"starlark",
							map[string]any{
								"code": "load('subprocess.star', 'subprocess')\nsubprocess.run(['block'])",
							},
						),
					)
				}
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Response: compactionResponse(gai.TextBlock("old answer")),
					},
					agenttest.GenerateStep{
						Response: compactionResponse(calls...),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if request.Dialog[0].Blocks[0].Content.String() != "old task detail" || !test.checkpointFailure && !request.Dialog[len(request.Dialog)-1].ToolResultError {
								return errors.New("invalid arguments erased history or succeeded")
							}
							return nil
						},
						Response: compactionResponse(gai.TextBlock("try a valid summary")),
					},
				)
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						if test.checkpointFailure {
							deps.History = rejectCompactionStore{
								Store: deps.History,
							}
						}
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				promptFixture(
					t,
					fixture.client,
					id,
					"old task detail",
				)
				finished := make(
					chan error,
					1,
				)
				go func() {
					response, err := fixture.client.Prompt(
						t.Context(),
						&acp.PromptRequest{
							SessionID: id,
							Prompt: []acp.ContentBlock{
								acp.TextContentBlock("/compact"),
							},
						},
					)
					if err == nil && test.cancelBatch && response.StopReason != acp.StopReasonCancelled {
						err = errors.New("turn was not cancelled")
					}
					finished <- err
				}()
				if test.cancelBatch {
					select {
					case <-fixture.commandStarted:
					case <-time.After(2 * time.Second):
						t.Fatal("following tool did not start")
					}
					if err := fixture.client.Cancel(
						t.Context(),
						&acp.CancelNotification{
							SessionID: id,
						},
					); err != nil {
						t.Fatal(err)
					}
				}
				err := <-finished
				if test.checkpointFailure {
					if err == nil || !strings.Contains(
						err.Error(),
						"compaction checkpoint unavailable",
					) {
						t.Fatalf(
							"checkpoint failure not reported: %v",
							err,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				wantCalls := 3
				if test.checkpointFailure || test.cancelBatch {
					wantCalls = 2
				}
				if len(generator.Requests()) != wantCalls {
					t.Fatalf(
						"model calls: got %d, want %d",
						len(generator.Requests()),
						wantCalls,
					)
				}
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil || record.Dialog[0].Blocks[0].Content.String() != "old task detail" {
					t.Fatalf(
						"old transcript lost: %#v, %v",
						record,
						err,
					)
				}
				if test.cancelBatch {
					if record.Compaction == nil || record.Compaction.Summary != "Saved goal." || record.Compaction.AfterMessages != len(record.Dialog) {
						t.Fatal("accepted summary not persisted at turn end")
					}
				} else if record.Compaction != nil {
					t.Fatal("failed compaction changed working context")
				}
				if test.checkpointFailure {
					// A later turn must still use the uncompacted live context after failed saves.
					promptFixture(
						t,
						fixture.client,
						id,
						"continue without compaction",
					)
				}
			},
		)
	}
}
