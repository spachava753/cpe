package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/models"
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
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				const summary = "Goal: update CPE. Next: finish the requested work."
				setup := compactionCall(
					t,
					"setup",
					"starlark_repl",
					map[string]any{
						"code": "counter = 1",
					},
				)
				compact := compactionCall(
					t,
					"compact-first",
					"compact",
					map[string]any{
						"summary": summary,
					},
				)
				increment := compactionCall(
					t,
					"increment",
					"starlark_repl",
					map[string]any{
						"code": "counter += 1\nprint(counter)",
					},
				)
				read := compactionCall(
					t,
					"read",
					"starlark_repl",
					map[string]any{
						"code": "print(counter)",
					},
				)
				if operation == "repeat compaction" {
					read = compactionCall(
						t,
						"compact-second",
						"compact",
						map[string]any{
							"summary": "Updated goal and next steps.",
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
							for _, tool := range request.Tools {
								if tool.Name == "compact" {
									schema := tool.InputSchema
									if len(schema.Properties) != 1 || schema.Properties["summary"].Type != "string" || len(schema.Required) != 1 || schema.Required[0] != "summary" {
										return errors.New("compact must take exactly one required summary string")
									}
									return nil
								}
							}
							return errors.New("compact tool unavailable from the first request")
						},
						Response: compactionResponse(setup),
					},
					agenttest.GenerateStep{
						Response: compactionResponse(gai.TextBlock("old answer")),
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if request.Dialog[len(request.Dialog)-1].Blocks[0].Content.String() != "Compact the conversation now by calling the compact tool." {
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
							if len(request.Dialog) != 4 || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\n"+summary || len(request.Dialog[1].Blocks) != 2 || request.Dialog[2].Blocks[0].ID != "compact-first" || request.Dialog[3].Blocks[0].ID != "increment" || request.Dialog[3].Blocks[0].Content.String() != "2\n" {
								return fmt.Errorf(
									"compaction lost context, tool pairing, or runtime state: %#v",
									request.Dialog,
								)
							}
							persisted, err := fixture.store.Load(
								context.Background(),
								SessionID(id),
							)
							if err != nil || persisted.Compaction == nil || persisted.Compaction.Summary != summary || persisted.Compaction.AfterMessages != 5 || len(persisted.Dialog) != 8 || persisted.Dialog[0].Blocks[0].Content.String() != "old task detail" {
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
							if len(request.Dialog) != 6 || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\n"+summary {
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
							case "repeat compaction":
								if len(request.Dialog) != 3 || request.Dialog[0].Blocks[0].Content.String() != "Summary of earlier conversation:\n\nUpdated goal and next steps." {
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
					func(context.Context, models.Model, ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
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
				case "load", "resume":
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
					SessionID(target),
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

// Reject only compaction checkpoints, allowing session setup and older history to persist.
type rejectCompactionStore struct {
	SessionStore
}

func (s rejectCompactionStore) Save(ctx context.Context, record SessionRecord) error {
	if record.Compaction != nil {
		return errors.New("compaction checkpoint unavailable")
	}
	return s.SessionStore.Save(
		ctx,
		record,
	)
}

func TestCompactionFailures(t *testing.T) {
	for _, test := range []struct {
		name              string
		arguments         map[string]any
		checkpointFailure bool
		cancelBatch       bool
		generationLimit   bool
	}{
		{
			name: "empty summary",
			arguments: map[string]any{
				"summary": "",
			},
		},
		{
			name: "whitespace summary",
			arguments: map[string]any{
				"summary": " \n\t",
			},
		},
		{
			name: "wrong type",
			arguments: map[string]any{
				"summary": 1,
			},
		},
		{
			name:      "missing summary",
			arguments: map[string]any{},
		},
		{
			name: "extra argument",
			arguments: map[string]any{
				"summary": "Saved goal.",
				"extra":   true,
			},
		},
		{
			name: "checkpoint failure prevents following generation",
			arguments: map[string]any{
				"summary": "Saved goal.",
			},
			checkpointFailure: true,
		},
		{
			name: "cancellation in a later tool preserves accepted summary",
			arguments: map[string]any{
				"summary": "Saved goal.",
			},
			cancelBatch: true,
		},
		{
			name: "generation limit preserves accepted summary",
			arguments: map[string]any{
				"summary": "Saved goal.",
			},
			generationLimit: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				calls := []gai.Block{
					compactionCall(
						t,
						"compact",
						"compact",
						test.arguments,
					),
				}
				if test.cancelBatch {
					calls = append(
						calls,
						compactionCall(
							t,
							"block",
							"starlark_repl",
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
					func(context.Context, models.Model, ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(config *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						if test.checkpointFailure {
							deps.History = rejectCompactionStore{
								SessionStore: deps.History,
							}
						}
						if test.generationLimit {
							config.MaxGenerations = 1
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
					if err == nil && test.generationLimit && response.StopReason != acp.StopReasonMaxTurnRequests {
						err = errors.New("turn did not stop at generation limit")
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
				if test.checkpointFailure || test.cancelBatch || test.generationLimit {
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
					SessionID(id),
				)
				if err != nil || record.Dialog[0].Blocks[0].Content.String() != "old task detail" {
					t.Fatalf(
						"old transcript lost: %#v, %v",
						record,
						err,
					)
				}
				if test.cancelBatch || test.generationLimit {
					if record.Compaction == nil || record.Compaction.Summary != "Saved goal." || record.Compaction.AfterMessages != 3 {
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

func TestCompactionCheckpointBoundary(t *testing.T) {
	call := compactionCall(
		t,
		"effect",
		"bash",
		map[string]any{
			"command": "pwd",
		},
	)
	for _, test := range []struct {
		name      string
		summary   string
		boundary  int
		wantError bool
	}{
		{
			name:     "complete batch",
			summary:  "Completed work.",
			boundary: 3,
		},
		{
			name:      "negative boundary",
			summary:   "Completed work.",
			boundary:  -1,
			wantError: true,
		},
		{
			name:      "past transcript",
			summary:   "Completed work.",
			boundary:  4,
			wantError: true,
		},
		{
			name:      "splits tool batch",
			summary:   "Completed work.",
			boundary:  2,
			wantError: true,
		},
		{
			name:      "empty summary",
			boundary:  3,
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				stored := storedRecord{
					Compaction: &Compaction{
						Summary:       test.summary,
						AfterMessages: test.boundary,
					},
				}
				for _, message := range []gai.Message{
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock("work"),
						},
					},
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							call,
						},
					},
					gai.ToolResultMessage(
						"effect",
						gai.TextBlock("/work"),
					),
				} {
					item, err := storeMessage(message)
					if err != nil {
						t.Fatal(err)
					}
					stored.Dialog = append(
						stored.Dialog,
						item,
					)
				}
				data, err := json.Marshal(stored)
				if err != nil {
					t.Fatal(err)
				}
				_, err = decodeRecord(data)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"decode checkpoint: %v",
						err,
					)
				}
			},
		)
	}
}
