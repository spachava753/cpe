package agent

import (
	"context"
	"errors"
	"io"
	"iter"
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

func TestResponseRecovery(t *testing.T) {
	for _, test := range []struct {
		name           string
		failures       int
		failure        error
		partial        bool
		notices        bool
		retries        uint
		wantCalls      int
		wantAborted    int
		wantError      bool
		failCheckpoint bool
	}{
		{
			name:        "partial stream retries with notice",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			notices:     true,
			retries:     1,
			wantCalls:   2,
			wantAborted: 1,
		},
		{
			name:        "partial stream retries with text fallback",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			retries:     1,
			wantCalls:   2,
			wantAborted: 1,
		},
		{
			name:        "transport failure before output",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			retries:     1,
			wantCalls:   2,
			wantAborted: 1,
		},
		{
			name:     "provider-wrapped transport failure retries",
			failures: 1,
			failure: &gai.ApiErr{
				Kind:  gai.APIErrorKindUnknown,
				Cause: io.ErrUnexpectedEOF,
			},
			retries:     1,
			wantCalls:   2,
			wantAborted: 1,
		},
		{
			name:     "permanent provider error is not retried",
			failures: 1,
			failure: &gai.ApiErr{
				Kind:       gai.APIErrorKindAuthentication,
				StatusCode: 401,
			},
			retries:     1,
			wantCalls:   1,
			wantAborted: 1,
			wantError:   true,
		},
		{
			name:     "credential failure is not retried after partial output",
			failures: 1,
			failure: errors.Join(
				credentialFailure(t),
				io.ErrUnexpectedEOF,
			),
			partial:     true,
			retries:     1,
			wantCalls:   1,
			wantAborted: 1,
			wantError:   true,
		},
		{
			name:        "retry limit exhausted",
			failures:    2,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			retries:     1,
			wantCalls:   2,
			wantAborted: 2,
			wantError:   true,
		},
		{
			name:           "checkpoint failure prevents retry",
			failures:       1,
			failure:        io.ErrUnexpectedEOF,
			partial:        true,
			retries:        1,
			wantCalls:      1,
			wantError:      true,
			failCheckpoint: true,
		},
		{
			name:        "retries disabled",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			retries:     0,
			wantCalls:   1,
			wantAborted: 1,
			wantError:   true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var steps []agenttest.StreamStep
				for range test.failures {
					step := agenttest.StreamStep{}
					if test.partial {
						step.Chunks = append(
							step.Chunks,
							gai.StreamChunk{
								Block: gai.TextBlock("aborted fragment"),
							},
						)
					}
					step.Chunks = append(
						step.Chunks,
						gai.StreamChunk{
							Err: test.failure,
						},
					)
					steps = append(
						steps,
						step,
					)
				}
				var history conversation.Store
				var id acp.SessionId
				steps = append(
					steps,
					agenttest.StreamStep{
						Check: func(request gai.GenerationRequest) error {
							record, err := history.Load(
								t.Context(),
								conversation.ID(id),
							)
							if err != nil {
								return err
							}
							if len(record.AbortedResponses) != test.failures {
								return errors.New("retry started before aborted response was checkpointed")
							}
							for _, message := range request.Dialog {
								for _, block := range message.Blocks {
									if strings.Contains(
										block.Content.String(),
										"aborted fragment",
									) {
										return errors.New("aborted output entered retry context")
									}
								}
							}
							return nil
						},
						Chunks: []gai.StreamChunk{
							{
								Block: gai.TextBlock("accepted answer"),
							},
						},
					},
				)
				generator := agenttest.NewScriptedStreamingGenerator(steps...)
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(config *Config, deps *Dependencies, initialize *acp.InitializeRequest) {
						config.MaxResponseRetries = test.retries
						if test.failCheckpoint {
							deps.History = rejectAbortedStore{
								Store: deps.History,
							}
						}
						if test.notices {
							initialize.ClientCapabilities = &acp.ClientCapabilities{
								Session: &acp.ClientSessionCapabilities{
									Notices: &acp.NoticeCapabilities{},
								},
							}
						}
					},
				)
				history = fixture.store
				id = openFixtureSession(
					t,
					fixture.client,
				)
				_, err := fixture.client.Prompt(
					t.Context(),
					&acp.PromptRequest{
						SessionID: id,
						Prompt: []acp.ContentBlock{
							acp.TextContentBlock("question"),
						},
					},
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"prompt: %v",
						err,
					)
				}
				if len(generator.Requests()) != test.wantCalls {
					t.Fatalf(
						"provider calls: %d",
						len(generator.Requests()),
					)
				}
				record, err := history.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil {
					t.Fatal(err)
				}
				if len(record.AbortedResponses) != test.wantAborted {
					t.Fatalf(
						"aborted responses: %d",
						len(record.AbortedResponses),
					)
				}
				for _, aborted := range record.AbortedResponses {
					if aborted.Model != "one" || aborted.At.IsZero() || aborted.Reason == "" || aborted.AfterMessages != 1 {
						t.Fatalf(
							"incomplete aborted record: %#v",
							aborted,
						)
					}
					if test.partial && (len(aborted.Partial.Blocks) != 1 || aborted.Partial.Blocks[0].Content.String() != "aborted fragment") {
						t.Fatal("aborted output not preserved")
					}
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				abortNotices := 0
				for _, notification := range fixture.notifications.updates {
					update := notification.Update
					if test.notices {
						if update.SessionUpdate == acp.SessionUpdateTypeNotice && update.Title != nil && *update.Title == "Model response aborted" {
							abortNotices++
						}
					} else if content, ok := update.Content.(acp.ContentBlock); ok && strings.Contains(
						content.Text,
						"Model response aborted",
					) {
						abortNotices++
					}
				}
				fixture.notifications.mu.Unlock()
				if abortNotices != test.wantAborted {
					t.Fatalf(
						"abort notices: %d",
						abortNotices,
					)
				}

				// A new server restores accepted conversation only, without resuming any attempt.
				var restoredCalls atomic.Int32
				restored := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generatorFunc(func(_ context.Context, request gai.GenerationRequest) (gai.Response, error) {
							restoredCalls.Add(1)
							for _, message := range request.Dialog {
								for _, block := range message.Blocks {
									if strings.Contains(
										block.Content.String(),
										"aborted fragment",
									) {
										t.Error("restored provider context includes aborted output")
									}
								}
							}
							return gai.Response{
								Candidates: []gai.Message{
									{
										Role: gai.Assistant,
										Blocks: []gai.Block{
											gai.TextBlock("continued"),
										},
									},
								},
								FinishReason: gai.EndTurn,
							}, nil
						}), gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, _ *acp.InitializeRequest) { deps.History = history },
				)
				if _, err := restored.client.LoadSession(
					t.Context(),
					&acp.LoadSessionRequest{
						SessionID: id,
						Cwd:       "/work",
					},
				); err != nil {
					t.Fatal(err)
				}
				if restoredCalls.Load() != 0 {
					t.Fatal("restoring a session retried an aborted response")
				}
				promptFixture(
					t,
					restored.client,
					id,
					"continue",
				)
				if err := restored.client.Close(); err != nil {
					t.Fatal(err)
				}
				restored.notifications.mu.Lock()
				defer restored.notifications.mu.Unlock()
				for _, notification := range restored.notifications.updates {
					if content, ok := notification.Update.Content.(acp.ContentBlock); ok && strings.Contains(
						content.Text,
						"aborted fragment",
					) {
						t.Fatal("aborted response replayed to ACP client")
					}
				}
			},
		)
	}
}

type rejectAbortedStore struct {
	conversation.Store
}

func (s rejectAbortedStore) Save(ctx context.Context, record conversation.Record) error {
	if len(record.AbortedResponses) != 0 {
		return errors.New("checkpoint unavailable")
	}
	return s.Store.Save(
		ctx,
		record,
	)
}

type streamingFunc func(context.Context, gai.GenerationRequest) iter.Seq[gai.StreamChunk]

func (f streamingFunc) Stream(ctx context.Context, request gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
	return f(
		ctx,
		request,
	)
}
func (f streamingFunc) Generate(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
	adapter := gai.StreamingAdapter{
		S: f,
	}
	return adapter.Generate(
		ctx,
		request,
	)
}

func TestResponseCancellation(t *testing.T) {
	for _, mode := range []string{
		"explicit cancel",
		"turn deadline",
	} {
		t.Run(
			mode,
			func(t *testing.T) {
				entered := make(
					chan struct{},
					1,
				)
				var calls atomic.Int32
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return streamingFunc(func(ctx context.Context, _ gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
							return func(yield func(gai.StreamChunk) bool) {
								calls.Add(1)
								if !yield(gai.StreamChunk{
									Block: gai.TextBlock("cancelled fragment"),
								}) {
									return
								}
								entered <- struct{}{}
								<-ctx.Done()
								yield(gai.StreamChunk{
									Err: ctx.Err(),
								})
							}
						}), gai.GenerationOptions{}, nil
					},
					func(config *Config, _ *Dependencies, _ *acp.InitializeRequest) {
						if mode == "turn deadline" {
							config.TurnTimeout = 100 * time.Millisecond
						}
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				done := make(
					chan error,
					1,
				)
				go func() {
					response, err := fixture.client.Prompt(
						t.Context(),
						&acp.PromptRequest{
							SessionID: id,
							Prompt: []acp.ContentBlock{
								acp.TextContentBlock("question"),
							},
						},
					)
					if err == nil && response.StopReason != acp.StopReasonCancelled {
						err = errors.New("turn did not report cancellation")
					}
					done <- err
				}()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("stream never started")
				}
				if mode == "explicit cancel" {
					if err := fixture.client.Cancel(
						t.Context(),
						&acp.CancelNotification{
							SessionID: id,
						},
					); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("cancelled stream did not finish")
				}
				if calls.Load() != 1 {
					t.Fatal("explicit cancellation or timeout retried the provider")
				}
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil {
					t.Fatal(err)
				}
				if len(record.AbortedResponses) != 1 || record.AbortedResponses[0].Partial.Blocks[0].Content.String() != "cancelled fragment" {
					t.Fatal("cancellation not persisted as aborted")
				}
			},
		)
	}
}

func TestResponseRetryPreservesToolEffects(t *testing.T) {
	generator := agenttest.NewScriptedStreamingGenerator(
		agenttest.StreamStep{
			Chunks: []gai.StreamChunk{
				{
					Block: gai.Block{
						ID:           "effect",
						BlockType:    gai.ToolCall,
						ModalityType: gai.Text,
						Content:      gai.Str("starlark_repl"),
					},
				},
				{
					Block: gai.Block{
						BlockType:    gai.ToolCall,
						ModalityType: gai.Text,
						Content:      gai.Str(`{"code":"load('subprocess.star', 'subprocess')\nsubprocess.run(['effect'])"}`),
					},
				},
			},
		},
		agenttest.StreamStep{
			Chunks: []gai.StreamChunk{
				{
					Block: gai.TextBlock("aborted reasoning"),
				},
				{
					Err: io.ErrUnexpectedEOF,
				},
			},
		},
		agenttest.StreamStep{
			Check: func(request gai.GenerationRequest) error {
				if request.Dialog[len(request.Dialog)-1].Role != gai.ToolResult {
					return errors.New("retry lost the completed tool result")
				}
				return nil
			},
			Chunks: []gai.StreamChunk{
				{
					Block: gai.TextBlock("done"),
				},
			},
		},
	)
	var effects atomic.Int32
	fixture := serverFixture(
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
						CommandRunner: runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
							effects.Add(1)
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
	promptFixture(
		t,
		fixture.client,
		id,
		"run the command",
	)
	if effects.Load() != 1 || len(generator.Requests()) != 3 {
		t.Fatalf(
			"effects=%d provider calls=%d",
			effects.Load(),
			len(generator.Requests()),
		)
	}
}
