package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/models"
)

func TestUsageReporting(t *testing.T) {
	for _, operation := range []string{
		"next turn",
		"model switch",
		"load",
		"resume",
		"fork",
		"isolated session",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				call, err := gai.ToolCallBlock(
					"compact",
					"compact",
					map[string]any{
						"summary": "Continue the current task.",
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				generator := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{
						Response: gai.Response{
							Candidates: []gai.Message{
								{
									Role: gai.Assistant,
									Blocks: []gai.Block{
										call,
									},
								},
							},
							FinishReason: gai.ToolUse,
							UsageMetadata: gai.Metadata{
								gai.UsageMetricInputTokens:      100,
								gai.UsageMetricGenerationTokens: 20,
								gai.UsageMetricCacheReadTokens:  80,
								gai.UsageMetricReasoningTokens:  5,
							},
						},
					},
					agenttest.GenerateStep{
						Check: func(request gai.GenerationRequest) error {
							if !strings.HasPrefix(
								request.Dialog[0].Blocks[0].Content.String(),
								"Summary of earlier conversation:",
							) {
								return errors.New("context was not compacted")
							}
							return nil
						},
						Response: usageResponse(gai.Metadata{
							gai.UsageMetricInputTokens:      30,
							gai.UsageMetricGenerationTokens: 10,
							gai.UsageMetricCacheReadTokens:  20,
							gai.UsageMetricReasoningTokens:  2,
						}),
					},
					agenttest.GenerateStep{
						Response: usageResponse(gai.Metadata{
							gai.UsageMetricInputTokens:      70,
							gai.UsageMetricGenerationTokens: 7,
							gai.UsageMetricCacheWriteTokens: 6,
						}),
					},
				)
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(config *Config, _ *Dependencies, _ *acp.InitializeRequest) {
						one := config.Models["one"]
						one.Limit = 1000
						config.Models["one"] = one
						two := config.Models["two"]
						two.Limit = 2000
						config.Models["two"] = two
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				first := usagePrompt(
					t,
					fixture.client,
					id,
				)
				wantFirst := &acp.Usage{
					InputTokens:      130,
					OutputTokens:     30,
					TotalTokens:      160,
					CachedReadTokens: new(uint64(100)),
					ThoughtTokens:    new(uint64(7)),
				}
				if diff := cmp.Diff(
					wantFirst,
					first.Usage,
				); diff != "" {
					t.Fatal(diff)
				}
				target := id
				switch operation {
				case "model switch":
					_, err = fixture.client.SetSessionConfigOption(
						t.Context(),
						&acp.SetSessionConfigOptionRequest{
							SessionID: id,
							ConfigID:  "model",
							Value:     "two",
						},
					)
				case "fork":
					fork, forkErr := fixture.client.ForkSession(
						t.Context(),
						&acp.ForkSessionRequest{
							SessionID: id,
							Cwd:       "/work",
						},
					)
					err = forkErr
					if fork != nil {
						target = fork.SessionID
					}
				case "load", "resume":
					_, err = fixture.client.CloseSession(
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
				case "isolated session":
					target = openFixtureSession(
						t,
						fixture.client,
					)
				}
				if err != nil {
					t.Fatal(err)
				}
				last := usagePrompt(
					t,
					fixture.client,
					target,
				)
				want := &acp.Usage{
					InputTokens:       200,
					OutputTokens:      37,
					TotalTokens:       237,
					CachedReadTokens:  new(uint64(100)),
					CachedWriteTokens: new(uint64(6)),
					ThoughtTokens:     new(uint64(7)),
				}
				if operation == "isolated session" {
					want = &acp.Usage{
						InputTokens:       70,
						OutputTokens:      7,
						TotalTokens:       77,
						CachedWriteTokens: new(uint64(6)),
					}
				}
				if diff := cmp.Diff(
					want,
					last.Usage,
				); diff != "" {
					t.Fatal(diff)
				}
				saved, err := fixture.store.Load(
					t.Context(),
					SessionID(target),
				)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(
					want,
					saved.Usage.protocolUsage(),
				); diff != "" {
					t.Fatal(diff)
				}
				if target != id {
					original, err := fixture.store.Load(
						t.Context(),
						SessionID(id),
					)
					if err != nil {
						t.Fatal(err)
					}
					if diff := cmp.Diff(
						wantFirst,
						original.Usage.protocolUsage(),
					); diff != "" {
						t.Fatal(diff)
					}
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				defer fixture.notifications.mu.Unlock()
				var updates []acp.UsageUpdate
				for _, notification := range fixture.notifications.updates {
					update := notification.Update
					if update.SessionUpdate == acp.SessionUpdateTypeUsageUpdate {
						updates = append(
							updates,
							acp.UsageUpdate{
								Used: update.Used,
								Size: update.Size,
								Cost: update.Cost,
							},
						)
					}
				}
				size := uint64(1000)
				if operation == "model switch" {
					size = 2000
				}
				if diff := cmp.Diff(
					[]acp.UsageUpdate{
						{
							Used: 120,
							Size: 1000,
						},
						{
							Used: 40,
							Size: 1000,
						},
						{
							Used: 77,
							Size: size,
						},
					},
					updates,
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}

func usageResponse(metadata gai.Metadata) gai.Response {
	return gai.Response{
		Candidates: []gai.Message{
			{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					gai.TextBlock("done"),
				},
			},
		},
		FinishReason:  gai.EndTurn,
		UsageMetadata: metadata,
	}
}

func usagePrompt(t *testing.T, client *acp.Client, id acp.SessionId) *acp.PromptResponse {
	t.Helper()
	response, err := client.Prompt(
		t.Context(),
		&acp.PromptRequest{
			SessionID: id,
			Prompt: []acp.ContentBlock{
				acp.TextContentBlock("continue"),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestUsageAttempts(t *testing.T) {
	for _, test := range []struct {
		name         string
		metadata     gai.Metadata
		retry        bool
		nonStreaming bool
		cancel       bool
		limit        uint
		want         *acp.Usage
		wantContext  []uint64
		wantError    bool
	}{
		{
			name:  "missing usage",
			limit: 1000,
		},
		{
			name: "missing output is unknown",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: 10,
			},
			limit: 1000,
		},
		{
			name: "explicit zeroes are reported",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      0,
				gai.UsageMetricGenerationTokens: 0,
				gai.UsageMetricCacheReadTokens:  0,
				gai.UsageMetricReasoningTokens:  0,
			},
			limit: 1000,
			want: &acp.Usage{
				CachedReadTokens: new(uint64(0)),
				ThoughtTokens:    new(uint64(0)),
			},
			wantContext: []uint64{
				0,
			},
		},
		{
			name: "usage without known window size",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      10,
				gai.UsageMetricGenerationTokens: 2,
			},
			want: &acp.Usage{
				InputTokens:  10,
				OutputTokens: 2,
				TotalTokens:  12,
			},
		},
		{
			name: "failed attempt and retry counted once each",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      10,
				gai.UsageMetricGenerationTokens: 2,
				gai.UsageMetricCacheReadTokens:  0,
			},
			retry: true,
			limit: 1000,
			want: &acp.Usage{
				InputTokens:      30,
				OutputTokens:     5,
				TotalTokens:      35,
				CachedReadTokens: new(uint64(0)),
				ThoughtTokens:    new(uint64(1)),
			},
			wantContext: []uint64{
				23,
			},
		},
		{
			name: "non-streaming failed attempt and retry",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      10,
				gai.UsageMetricGenerationTokens: 2,
			},
			retry:        true,
			nonStreaming: true,
			limit:        1000,
			want: &acp.Usage{
				InputTokens:   30,
				OutputTokens:  5,
				TotalTokens:   35,
				ThoughtTokens: new(uint64(1)),
			},
			wantContext: []uint64{
				23,
			},
		},
		{
			name: "cancellation preserves consumed tokens but not aborted output in context",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      10,
				gai.UsageMetricGenerationTokens: 2,
			},
			cancel: true,
			limit:  1000,
			want: &acp.Usage{
				InputTokens:  10,
				OutputTokens: 2,
				TotalTokens:  12,
			},
		},
		{
			name: "invalid metadata does not retry",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      -1,
				gai.UsageMetricGenerationTokens: 2,
			},
			limit:     1000,
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				ready := make(chan struct{})
				var calls atomic.Int32
				var generator gai.Generator = streamingFunc(func(ctx context.Context, _ gai.GenerationRequest) iter.Seq[gai.StreamChunk] {
					return func(yield func(gai.StreamChunk) bool) {
						attempt := calls.Add(1)
						if !yield(gai.StreamChunk{
							Block: gai.TextBlock("text"),
						}) {
							return
						}
						if attempt > 1 {
							yield(gai.StreamChunk{
								Block: gai.MetadataBlock(gai.Metadata{
									gai.UsageMetricInputTokens:      20,
									gai.UsageMetricGenerationTokens: 3,
									gai.UsageMetricReasoningTokens:  1,
								}),
							})
							return
						}
						if test.retry {
							// Metadata is a snapshot, not an additive delta within an attempt.
							if !yield(gai.StreamChunk{
								Block: gai.MetadataBlock(gai.Metadata{
									gai.UsageMetricInputTokens:      10,
									gai.UsageMetricGenerationTokens: 1,
								}),
							}) {
								return
							}
						}
						if test.metadata != nil && !yield(gai.StreamChunk{
							Block: gai.MetadataBlock(test.metadata),
						}) {
							return
						}
						if test.cancel {
							close(ready)
							<-ctx.Done()
							yield(gai.StreamChunk{
								Err: ctx.Err(),
							})
						} else if test.retry {
							yield(gai.StreamChunk{
								Err: io.ErrUnexpectedEOF,
							})
						}
					}
				})
				if test.nonStreaming {
					generator = generatorFunc(func(context.Context, gai.GenerationRequest) (gai.Response, error) {
						if calls.Add(1) == 1 {
							return usageResponse(test.metadata), io.ErrUnexpectedEOF
						}
						return usageResponse(gai.Metadata{
							gai.UsageMetricInputTokens:      20,
							gai.UsageMetricGenerationTokens: 3,
							gai.UsageMetricReasoningTokens:  1,
						}), nil
					})
				}
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return generator, gai.GenerationOptions{}, nil
					},
					func(config *Config, _ *Dependencies, _ *acp.InitializeRequest) {
						model := config.Models["one"]
						model.Limit = test.limit
						config.Models["one"] = model
					},
				)
				id := openFixtureSession(
					t,
					fixture.client,
				)
				var response *acp.PromptResponse
				var promptErr error
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					response, promptErr = fixture.client.Prompt(
						t.Context(),
						&acp.PromptRequest{
							SessionID: id,
							Prompt: []acp.ContentBlock{
								acp.TextContentBlock("continue"),
							},
						},
					)
				}()
				if test.cancel {
					select {
					case <-ready:
					case <-time.After(2 * time.Second):
						t.Fatal("metadata was not consumed")
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
				<-finished
				if test.wantError {
					if promptErr == nil || !strings.Contains(
						promptErr.Error(),
						"invalid provider token usage",
					) {
						t.Fatalf(
							"invalid metadata: %v",
							promptErr,
						)
					}
				} else {
					if promptErr != nil {
						t.Fatal(promptErr)
					}
					if test.cancel && response.StopReason != acp.StopReasonCancelled {
						t.Fatal("turn did not cancel")
					}
					if diff := cmp.Diff(
						test.want,
						response.Usage,
					); diff != "" {
						t.Fatal(diff)
					}
				}
				wantCalls := int32(1)
				if test.retry {
					wantCalls = 2
				}
				if calls.Load() != wantCalls {
					t.Fatal("unexpected provider attempts")
				}
				record, err := fixture.store.Load(
					t.Context(),
					SessionID(id),
				)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(
					test.want,
					record.Usage.protocolUsage(),
				); diff != "" {
					t.Fatal(diff)
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				defer fixture.notifications.mu.Unlock()
				var contextUsed []uint64
				for _, notification := range fixture.notifications.updates {
					if notification.Update.SessionUpdate == acp.SessionUpdateTypeUsageUpdate {
						contextUsed = append(
							contextUsed,
							notification.Update.Used,
						)
					}
				}
				if diff := cmp.Diff(
					test.wantContext,
					contextUsed,
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}

func TestUsageStopReasons(t *testing.T) {
	for _, test := range []struct {
		name   string
		finish gai.FinishReason
		want   acp.StopReason
	}{
		{
			name:   "end turn",
			finish: gai.EndTurn,
			want:   acp.StopReasonEndTurn,
		},
		{
			name:   "output limit",
			finish: gai.MaxGenerationLimit,
			want:   acp.StopReasonMaxTokens,
		},
		{
			name:   "refusal",
			finish: gai.ContentPolicyViolation,
			want:   acp.StopReasonRefusal,
		},
		{
			name:   "generation limit after compaction",
			finish: gai.ToolUse,
			want:   acp.StopReasonMaxTurnRequests,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				response := usageResponse(gai.Metadata{
					gai.UsageMetricInputTokens:      100,
					gai.UsageMetricGenerationTokens: 20,
				})
				response.FinishReason = test.finish
				if test.finish == gai.ToolUse {
					call, err := gai.ToolCallBlock(
						"compact",
						"compact",
						map[string]any{
							"summary": "Current task and next steps.",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					response.Candidates[0].Blocks[0] = call
				}
				fixture := serverFixture(
					t,
					func(context.Context, models.Model, ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						return agenttest.NewScriptedGenerator(agenttest.GenerateStep{
							Response: response,
						}), gai.GenerationOptions{}, nil
					},
					func(config *Config, _ *Dependencies, _ *acp.InitializeRequest) {
						config.MaxGenerations = 1
					},
				)
				actual := usagePrompt(
					t,
					fixture.client,
					openFixtureSession(
						t,
						fixture.client,
					),
				)
				if diff := cmp.Diff(
					&acp.PromptResponse{
						StopReason: test.want,
						Usage: &acp.Usage{
							InputTokens:  100,
							OutputTokens: 20,
							TotalTokens:  120,
						},
					},
					actual,
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}

func TestTokenUsage(t *testing.T) {
	for _, test := range []struct {
		name      string
		previous  TokenUsage
		metadata  gai.Metadata
		wantError bool
		wantInput uint64
	}{
		{
			name: "preserves exact JSON integers",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: json.Number("9007199254740993"),
			},
			wantInput: 9007199254740993,
		},
		{
			name: "adding does not mutate the earlier checkpoint",
			previous: TokenUsage{
				InputTokens: new(uint64(10)),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: 7,
			},
			wantInput: 17,
		},
		{
			name: "counter overflow",
			previous: TokenUsage{
				InputTokens: new(uint64(math.MaxUint64)),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: 1,
			},
			wantError: true,
		},
		{
			name: "total overflow",
			previous: TokenUsage{
				InputTokens: new(uint64(math.MaxUint64)),
			},
			metadata: gai.Metadata{
				gai.UsageMetricGenerationTokens: 1,
			},
			wantError: true,
		},
		{
			name: "negative token count",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: -1,
			},
			wantError: true,
		},
		{
			name: "fractional token count",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: json.Number("1.5"),
			},
			wantError: true,
		},
		{
			name: "wrong token type",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: "12",
			},
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				before, err := json.Marshal(test.previous)
				if err != nil {
					t.Fatal(err)
				}
				delta, err := parseTokenUsage(test.metadata)
				var actual TokenUsage
				if err == nil {
					actual, err = test.previous.add(delta)
				}
				if (err != nil) != test.wantError {
					t.Fatalf(
						"usage error: %v",
						err,
					)
				}
				if err == nil && (actual.InputTokens == nil || *actual.InputTokens != test.wantInput) {
					t.Fatalf(
						"input tokens: %#v",
						actual,
					)
				}
				after, err := json.Marshal(test.previous)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("accumulation mutated an earlier checkpoint")
				}
			},
		)
	}
}
