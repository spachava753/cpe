package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/negrel/secrecy"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestProviders(t *testing.T) {
	for _, test := range []struct {
		name        string
		base        string
		api         models.API
		expired     bool
		explicit    bool
		wantKey     string
		wantError   bool
		stream      bool
		toolResults bool
	}{
		{
			name:    "ChatGPT subscription",
			base:    "https://chatgpt.com/backend-api/codex/",
			api:     models.Responses,
			wantKey: "access-fixture",
		},
		{
			name:      "expired subscription",
			base:      "https://chatgpt.com/backend-api/codex/",
			api:       models.Responses,
			expired:   true,
			wantError: true,
		},
		{
			name:    "OpenCode subscription",
			base:    "https://opencode.ai/zen/go/v1/",
			api:     models.Openai,
			wantKey: "opencode-fixture",
		},
		{
			name: "no token to lookalike host",
			base: "https://chatgpt.com.invalid/v1/",
			api:  models.Responses,
		},
		{
			name: "no token over HTTP",
			base: "http://chatgpt.com/backend-api/codex/",
			api:  models.Responses,
		},
		{
			name:      "no token to unrelated endpoint",
			base:      "https://example.invalid/v1/",
			api:       models.Openai,
			wantError: true,
		},
		{
			name:     "Anthropic ignores ambient credentials",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "explicit ChatGPT API key bypasses OAuth refresh",
			base:     "https://chatgpt.com/backend-api/codex/",
			api:      models.Responses,
			expired:  true,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "explicit key takes precedence",
			base:     "https://opencode.ai/zen/go/v1/",
			api:      models.Openai,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Chat Completions streaming cache key",
			base:     "https://proxy.invalid/v1/",
			api:      models.Openai,
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Responses streaming cache key",
			base:     "https://proxy.invalid/v1/",
			api:      models.Responses,
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Anthropic streaming caching",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:        "Anthropic tool result caching",
			base:        "https://proxy.invalid/",
			api:         models.Anthropic,
			explicit:    true,
			wantKey:     "explicit-fixture",
			toolResults: true,
		},
		{
			name:        "Anthropic streaming tool result caching",
			base:        "https://proxy.invalid/",
			api:         models.Anthropic,
			explicit:    true,
			wantKey:     "explicit-fixture",
			stream:      true,
			toolResults: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.api == models.Anthropic {
					t.Setenv(
						"ANTHROPIC_AUTH_TOKEN",
						"ambient-fixture-must-not-be-sent",
					)
				}
				filesystem := afero.NewMemMapFs()
				store := auth.NewStore(filesystem)
				expires := time.Now().Add(time.Hour)
				if test.expired {
					expires = time.Now().Add(-time.Hour)
				}
				credentials := auth.Credentials{
					OpenAI: &auth.OAuthCredentials{
						AccessToken:  "access-fixture",
						RefreshToken: "refresh-fixture",
						IDToken:      "id-fixture",
						AccountID:    "account-fixture",
						ExpiresAt:    expires,
					},
					OpenCode: &auth.APIKeyCredentials{
						APIKey: "opencode-fixture",
					},
				}
				for _, provider := range []auth.Provider{
					auth.OpenAI,
					auth.OpenCode,
				} {
					if err := store.Save(
						t.Context(),
						"/auth.json",
						provider,
						credentials,
					); err != nil {
						t.Fatal(err)
					}
				}
				called := false
				var toolResultPrefix string
				providers, err := New(
					store,
					"/auth.json",
					&http.Client{
						Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
							if request.URL.Host == "auth.openai.com" {
								if !test.expired || test.explicit {
									t.Error("unexpected OAuth refresh")
								}
								return &http.Response{
									StatusCode: http.StatusBadRequest,
									Body:       io.NopCloser(strings.NewReader("{}")),
								}, nil
							}
							called = true
							key := strings.TrimPrefix(
								request.Header.Get("Authorization"),
								"Bearer ",
							)
							if test.api == models.Anthropic {
								if request.Header.Get("Authorization") != "" {
									t.Error("ambient bearer token leaked to configured endpoint")
								}
								key = request.Header.Get("X-Api-Key")
							}
							if key != test.wantKey {
								t.Error("incorrect credential routing")
							}
							if test.wantKey == "access-fixture" && request.Header.Get("ChatGPT-Account-Id") != "account-fixture" {
								t.Error("missing account header")
							}
							type cacheBlock struct {
								Type         string            `json:"type"`
								CacheControl map[string]string `json:"cache_control"`
								ToolUseID    string            `json:"tool_use_id"`
								Content      json.RawMessage   `json:"content"`
							}
							var body struct {
								ReasoningEffort string       `json:"reasoning_effort"`
								PromptCacheKey  string       `json:"prompt_cache_key"`
								System          []cacheBlock `json:"system"`
								Messages        []struct {
									Content json.RawMessage `json:"content"`
								} `json:"messages"`
							}
							if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if test.api == models.Openai && body.ReasoningEffort != "high" {
								t.Error("reasoning effort was not applied")
							}
							if (test.api == models.Openai || test.api == models.Responses) && body.PromptCacheKey != "conversation-fixture" {
								t.Errorf(
									"prompt cache key: %q",
									body.PromptCacheKey,
								)
							}
							if test.api == models.Anthropic {
								if len(body.System) != 1 || body.System[0].CacheControl["type"] != "ephemeral" || body.System[0].CacheControl["ttl"] != "" {
									t.Errorf(
										"system cache control: %#v",
										body.System,
									)
								}
								if len(body.Messages) == 0 {
									t.Error("missing conversation messages")
								} else {
									var content []cacheBlock
									if err := json.Unmarshal(
										body.Messages[len(body.Messages)-1].Content,
										&content,
									); err != nil {
										t.Error(err)
									}
									if len(content) == 0 || content[len(content)-1].CacheControl["type"] != "ephemeral" || content[len(content)-1].CacheControl["ttl"] != "" {
										t.Errorf(
											"conversation cache control: %#v",
											content,
										)
									}
									if len(content) == 2 && content[0].Type == "tool_result" && content[0].CacheControl != nil {
										t.Errorf(
											"cache breakpoint must follow all parallel tool results: %#v",
											content,
										)
									}
								}
								if test.toolResults {
									found := false
									for _, message := range body.Messages {
										var content []cacheBlock
										if err := json.Unmarshal(
											message.Content,
											&content,
										); err != nil {
											t.Error(err)
										}
										if len(content) == 0 || content[0].Type != "tool_result" {
											continue
										}
										found = true
										if len(content) != 2 || content[0].ToolUseID != "first" || content[1].ToolUseID != "second" {
											t.Errorf(
												"unstable tool result order: %#v",
												content,
											)
										}
										// Cache directives move to the newest block as the conversation grows.
										for i := range content {
											content[i].CacheControl = nil
										}
										prefix, err := json.Marshal(content)
										if err != nil {
											t.Error(err)
										}
										if toolResultPrefix == "" {
											toolResultPrefix = string(prefix)
										} else if string(prefix) != toolResultPrefix {
											t.Errorf(
												"unchanged tool result prefix changed: %s != %s",
												prefix,
												toolResultPrefix,
											)
										}
									}
									if !found {
										t.Error("missing parallel tool results")
									}
								}
							}
							response := `{"id":"fixture","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
							if test.api == models.Anthropic {
								response = `{"id":"fixture","type":"message","role":"assistant","model":"fixture","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
							}
							if test.api == models.Responses {
								response = `{"id":"fixture","object":"response","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}]}`
							}
							contentType := "application/json"
							if test.stream {
								contentType = "text/event-stream"
								switch test.api {
								case models.Openai:
									response = "data: " + `{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
								case models.Responses:
									response = "data: " + `{"type":"response.output_text.delta","delta":"ok"}` + "\n\ndata: " + `{"type":"response.completed","response":` + response + "}\n\n"
								case models.Anthropic:
									response = "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"fixture","type":"message","role":"assistant","model":"fixture","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n" +
										"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
										"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
										"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n" +
										"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` + "\n\n" +
										"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
								}
							}
							return &http.Response{
								StatusCode: http.StatusOK,
								Header: http.Header{
									"Content-Type": []string{
										contentType,
									},
								},
								Body: io.NopCloser(strings.NewReader(response)),
							}, nil
						}),
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(providers.Close)
				base, err := url.Parse(test.base)
				if err != nil {
					t.Fatal(err)
				}
				model := models.Model{
					ID:        "fixture",
					BaseUrl:   *base,
					API:       test.api,
					MaxTokens: 64,
				}
				if test.explicit {
					secret := secrecy.NewSecretString([]byte("explicit-fixture"))
					model.APIKey = &secret
				}
				generator, options, err := providers.Generator(
					t.Context(),
					model,
					"high",
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"generator: %v",
						err,
					)
				}
				if test.wantError {
					if called {
						t.Fatal("model HTTP called before generator configuration succeeded")
					}
					return
				}
				dialog := gai.Dialog{
					{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock("hello"),
						},
					},
				}
				if test.toolResults {
					var calls []gai.Block
					for _, id := range []string{
						"first",
						"second",
					} {
						call, err := gai.ToolCallBlock(
							id,
							"lookup",
							map[string]any{},
						)
						if err != nil {
							t.Fatal(err)
						}
						calls = append(
							calls,
							call,
						)
					}
					dialog = append(
						dialog,
						gai.Message{
							Role:   gai.Assistant,
							Blocks: calls,
						},
						gai.ToolResultMessage(
							"first",
							gai.TextBlock("first result"),
						),
						gai.ToolResultMessage(
							"second",
							gai.TextBlock("second result"),
						),
					)
				}
				if test.stream {
					generator = &gai.StreamingAdapter{
						S: generator.(gai.StreamingGenerator),
					}
				}
				request := gai.GenerationRequest{
					Model:          "fixture",
					PromptCacheKey: "conversation-fixture",
					Instructions:   gai.SystemMessage(gai.TextBlock("system fixture")),
					Dialog:         dialog,
					Options:        options,
				}
				repetitions := 1
				if test.toolResults {
					// Repeated serialization exposes nondeterministic tool-result map iteration.
					repetitions = 64
				}
				for attempt := range repetitions {
					if test.toolResults && attempt == repetitions/2 {
						request.Dialog = append(
							request.Dialog,
							gai.Message{
								Role: gai.Assistant,
								Blocks: []gai.Block{
									gai.TextBlock("ok"),
								},
							},
							gai.Message{
								Role: gai.User,
								Blocks: []gai.Block{
									gai.TextBlock("continue"),
								},
							},
						)
					}
					if _, err := generator.Generate(
						t.Context(),
						request,
					); err != nil {
						t.Fatal(err)
					}
				}
				if !called {
					t.Fatal("provider did not use injected HTTP client")
				}
				if err := providers.Logout(context.Background()); err != nil {
					t.Fatal(err)
				}
				remaining, err := store.Retrieve(
					t.Context(),
					"/auth.json",
				)
				if err != nil || remaining.OpenAI != nil || remaining.OpenCode != nil {
					t.Fatal("logout did not clear local credentials")
				}
			},
		)
	}
}
