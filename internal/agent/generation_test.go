package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
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
		name         string
		failures     int
		failure      error
		partial      bool
		nonStreaming bool
		notices      bool
		retries      uint
		wantCalls    int
		wantUpdates  int
		wantError    bool
	}{
		{
			name:        "partial stream retries with message and notice",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			notices:     true,
			retries:     1,
			wantCalls:   2,
			wantUpdates: 1,
		},
		{
			name:        "partial stream retries with message only",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			retries:     1,
			wantCalls:   2,
			wantUpdates: 1,
		},
		{
			name:         "non-streaming partial response retries with message and notice",
			failures:     1,
			failure:      errors.New("interrupted generation"),
			partial:      true,
			nonStreaming: true,
			notices:      true,
			retries:      1,
			wantCalls:    2,
			wantUpdates:  1,
		},
		{
			name:        "transport failure before output",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			retries:     1,
			wantCalls:   2,
			wantUpdates: 1,
		},
		{
			name:        "unknown failure after partial output retries",
			failures:    1,
			failure:     errors.New("interrupted generation"),
			partial:     true,
			retries:     1,
			wantCalls:   2,
			wantUpdates: 1,
		},
		{
			name:        "unknown failure before output is not retried",
			failures:    1,
			failure:     errors.New("invalid generation configuration"),
			retries:     1,
			wantCalls:   1,
			wantUpdates: 1,
			wantError:   true,
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
			wantUpdates: 1,
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
			wantUpdates: 1,
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
			wantUpdates: 1,
			wantError:   true,
		},
		{
			name:        "retry limit exhausted",
			failures:    2,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			notices:     true,
			retries:     1,
			wantCalls:   2,
			wantUpdates: 2,
			wantError:   true,
		},
		{
			name:        "retries disabled",
			failures:    1,
			failure:     io.ErrUnexpectedEOF,
			partial:     true,
			retries:     0,
			wantCalls:   1,
			wantUpdates: 1,
			wantError:   true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				directory := t.TempDir()
				history, err := conversation.NewStore(directory)
				if err != nil {
					t.Fatal(err)
				}
				var id acp.SessionId
				var checkpoint []byte
				check := func(request gai.GenerationRequest) error {
					data, err := os.ReadFile(filepath.Join(
						directory,
						string(id)+".json",
					))
					if err != nil {
						return err
					}
					if checkpoint == nil {
						checkpoint = data
					} else if !bytes.Equal(
						checkpoint,
						data,
					) {
						return errors.New("retry changed the persisted checkpoint")
					}
					if len(request.Dialog) != 1 || request.Dialog[0].Role != gai.User || request.Dialog[0].Blocks[0].Content.String() != "question" {
						return errors.New("attempt did not use only the accepted conversation")
					}
					return nil
				}
				var partial gai.Message
				if test.partial {
					partial = gai.Message{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							gai.TextBlock("aborted fragment"),
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								Content:      gai.Str("aborted reasoning"),
							},
						},
					}
				}
				var steps []agenttest.StreamStep
				var nonStreamingSteps []agenttest.GenerateStep
				for range test.failures {
					step := agenttest.StreamStep{
						Check: check,
					}
					for _, block := range partial.Blocks {
						step.Chunks = append(
							step.Chunks,
							gai.StreamChunk{
								Block: block,
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
					nonStreamingSteps = append(
						nonStreamingSteps,
						agenttest.GenerateStep{
							Check: check,
							Response: gai.Response{
								Candidates: []gai.Message{
									partial,
								},
							},
							Err: test.failure,
						},
					)
				}
				steps = append(
					steps,
					agenttest.StreamStep{
						Check: check,
						Chunks: []gai.StreamChunk{
							{
								Block: gai.TextBlock("accepted answer"),
							},
						},
					},
				)
				streaming := agenttest.NewScriptedStreamingGenerator(steps...)
				var generator gai.Generator = streaming
				requests := streaming.Requests
				if test.nonStreaming {
					nonStreamingSteps = append(
						nonStreamingSteps,
						agenttest.GenerateStep{
							Check:    check,
							Response: compactionResponse(gai.TextBlock("accepted answer")),
						},
					)
					nonStreaming := agenttest.NewScriptedGenerator(nonStreamingSteps...)
					generator = nonStreaming
					requests = nonStreaming.Requests
				}
				fixture := serverFixture(
					t,
					func(context.Context, conversation.ID, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(config *Config, deps *Dependencies, initialize *acp.InitializeRequest) {
						config.MaxResponseRetries = test.retries
						deps.History = history
						if test.notices {
							initialize.ClientCapabilities = &acp.ClientCapabilities{
								Session: &acp.ClientSessionCapabilities{
									Notices: &acp.NoticeCapabilities{},
								},
							}
						}
					},
				)
				id = openFixtureSession(
					t,
					fixture.client,
				)
				_, err = fixture.client.Prompt(
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
				if len(requests()) != test.wantCalls {
					t.Fatalf(
						"provider calls: %d",
						len(requests()),
					)
				}
				for _, request := range requests() {
					if request.PromptCacheKey != string(id) {
						t.Fatalf(
							"attempt lost the conversation cache key: %q",
							request.PromptCacheKey,
						)
					}
				}
				record, err := history.Load(
					t.Context(),
					conversation.ID(id),
				)
				if err != nil {
					t.Fatal(err)
				}
				wantMessages := 2
				if test.wantError {
					wantMessages = 1
				}
				if len(record.Dialog) != wantMessages || record.Dialog[0].Blocks[0].Content.String() != "question" {
					t.Fatalf(
						"accepted history: %#v",
						record.Dialog,
					)
				}
				data, err := os.ReadFile(filepath.Join(
					directory,
					string(id)+".json",
				))
				if err != nil {
					t.Fatal(err)
				}
				for _, fragment := range []string{
					"aborted fragment",
					"aborted reasoning",
					"The model call was aborted",
					"Model response aborted",
				} {
					if bytes.Contains(
						data,
						[]byte(fragment),
					) {
						t.Fatalf(
							"checkpoint retained %q",
							fragment,
						)
					}
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				updates := slices.Clone(fixture.notifications.updates)
				fixture.notifications.mu.Unlock()
				abortMessages, abortNotices, retryMessages := 0, 0, 0
				for _, notification := range updates {
					update := notification.Update
					if update.SessionUpdate == acp.SessionUpdateTypeNotice && update.Title != nil && *update.Title == "Model response aborted" {
						abortNotices++
					}
					if content, ok := update.Content.(acp.ContentBlock); ok && update.SessionUpdate == acp.SessionUpdateTypeAgentMessageChunk && strings.Contains(
						content.Text,
						"The model call was aborted",
					) {
						abortMessages++
						if strings.Contains(
							content.Text,
							"Retrying the model response",
						) {
							retryMessages++
						}
					}
				}
				wantNotices := 0
				if test.notices {
					wantNotices = test.wantUpdates
				}
				if abortMessages != test.wantUpdates || abortNotices != wantNotices || retryMessages != test.wantCalls-1 {
					t.Fatalf(
						"abort messages=%d notices=%d retry messages=%d",
						abortMessages,
						abortNotices,
						retryMessages,
					)
				}

				// A new server restores accepted conversation only, without resuming any attempt.
				var restoredCalls atomic.Int32
				restored := serverFixture(
					t,
					func(context.Context, conversation.ID, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generatorFunc(func(_ context.Context, request gai.GenerationRequest) (gai.Response, error) {
							restoredCalls.Add(1)
							if len(request.Dialog) != wantMessages+1 || request.Dialog[0].Blocks[0].Content.String() != "question" {
								t.Error("restored provider context did not retain accepted history")
							}
							return compactionResponse(gai.TextBlock("continued")), nil
						}), gai.GenerationOptions{}, nil
					},
					func(_ *Config, deps *Dependencies, initialize *acp.InitializeRequest) {
						deps.History = history
						if test.notices {
							initialize.ClientCapabilities = &acp.ClientCapabilities{
								Session: &acp.ClientSessionCapabilities{
									Notices: &acp.NoticeCapabilities{},
								},
							}
						}
					},
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
				replayedAnswer := false
				for _, notification := range restored.notifications.updates {
					update := notification.Update
					if update.SessionUpdate == acp.SessionUpdateTypeNotice && update.Title != nil && *update.Title == "Model response aborted" {
						t.Fatal("abort notice replayed to ACP client")
					}
					if content, ok := update.Content.(acp.ContentBlock); ok {
						for _, fragment := range []string{
							"aborted fragment",
							"aborted reasoning",
							"The model call was aborted",
						} {
							if strings.Contains(
								content.Text,
								fragment,
							) {
								t.Fatalf(
									"replay retained %q",
									fragment,
								)
							}
						}
						if content.Text == "accepted answer" {
							replayedAnswer = true
						}
					}
				}
				if replayedAnswer == test.wantError {
					t.Fatal("accepted answer was not replayed as expected")
				}
			},
		)
	}
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
	entered := make(
		chan struct{},
		1,
	)
	var calls atomic.Int32
	fixture := serverFixture(
		t,
		func(context.Context, conversation.ID, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
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
	if err := fixture.client.Cancel(
		t.Context(),
		&acp.CancelNotification{
			SessionID: id,
		},
	); err != nil {
		t.Fatal(err)
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
		t.Fatal("explicit cancellation retried the provider")
	}
	record, err := fixture.store.Load(
		t.Context(),
		conversation.ID(id),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Dialog) != 1 || record.Dialog[0].Role != gai.User || record.Dialog[0].Blocks[0].Content.String() != "question" {
		t.Fatalf(
			"cancellation changed accepted history: %#v",
			record.Dialog,
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
						Content:      gai.Str("starlark"),
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
		func(context.Context, conversation.ID, models.Model, models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
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
