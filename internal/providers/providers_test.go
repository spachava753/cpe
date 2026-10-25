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
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/version"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestProviders(t *testing.T) {
	for _, test := range []struct {
		name        string
		base        string
		effort      models.ReasoningLevel
		wantKey     string
		wantSession string
		api         models.API
		expired     bool
		explicit    bool
		noSession   bool
		wantError   bool
		stream      bool
		toolResults bool
	}{
		{
			name:    "ChatGPT subscription",
			base:    "https://chatgpt.com/backend-api/codex/",
			api:     models.Responses,
			effort:  "high",
			wantKey: "access-fixture",
		},
		{
			name:      "expired subscription",
			base:      "https://chatgpt.com/backend-api/codex/",
			api:       models.Responses,
			effort:    "high",
			expired:   true,
			wantError: true,
		},
		{
			name:        "OpenCode subscription",
			base:        "https://opencode.ai/zen/go/v1/",
			api:         models.Openai,
			effort:      "high",
			wantKey:     "opencode-fixture",
			wantSession: "conversation-fixture",
		},
		{
			name:        "OpenCode Chat Completions streaming",
			base:        "https://opencode.ai/zen/go/v1/",
			api:         models.Openai,
			effort:      "high",
			wantKey:     "opencode-fixture",
			wantSession: "conversation-fixture",
			stream:      true,
		},
		{
			name:        "OpenCode Anthropic subscription",
			base:        "https://opencode.ai/zen/go/",
			api:         models.Anthropic,
			effort:      "high",
			wantKey:     "opencode-fixture",
			wantSession: "conversation-fixture",
		},
		{
			name:        "OpenCode Anthropic explicit key streaming",
			base:        "https://opencode.ai/zen/go/",
			api:         models.Anthropic,
			effort:      "high",
			explicit:    true,
			wantKey:     "explicit-fixture",
			wantSession: "conversation-fixture",
			stream:      true,
		},
		{
			name:        "OpenCode Responses subscription",
			base:        "https://opencode.ai/zen/go/v1/",
			api:         models.Responses,
			effort:      "high",
			wantKey:     "opencode-fixture",
			wantSession: "conversation-fixture",
		},
		{
			name:        "OpenCode Responses explicit key streaming",
			base:        "https://opencode.ai/zen/go/v1/",
			api:         models.Responses,
			effort:      "high",
			explicit:    true,
			wantKey:     "explicit-fixture",
			wantSession: "conversation-fixture",
			stream:      true,
		},
		{
			name:      "OpenCode requires a conversation ID",
			base:      "https://opencode.ai/zen/go/v1/",
			api:       models.Openai,
			effort:    "high",
			explicit:  true,
			noSession: true,
			wantError: true,
		},
		{
			name:   "no OpenCode headers to lookalike host",
			base:   "https://opencode.ai.invalid/v1/",
			api:    models.Responses,
			effort: "high",
		},
		{
			name:   "no OpenCode headers over HTTP",
			base:   "http://opencode.ai/zen/go/v1/",
			api:    models.Responses,
			effort: "high",
		},
		{
			name:   "no token to lookalike host",
			base:   "https://chatgpt.com.invalid/v1/",
			api:    models.Responses,
			effort: "high",
		},
		{
			name:   "no token over HTTP",
			base:   "http://chatgpt.com/backend-api/codex/",
			api:    models.Responses,
			effort: "high",
		},
		{
			name:      "no token to unrelated endpoint",
			base:      "https://example.invalid/v1/",
			api:       models.Openai,
			effort:    "high",
			wantError: true,
		},
		{
			name:     "Anthropic ignores ambient credentials",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "high",
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "explicit ChatGPT API key bypasses OAuth refresh",
			base:     "https://chatgpt.com/backend-api/codex/",
			api:      models.Responses,
			effort:   "high",
			expired:  true,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:        "explicit key takes precedence",
			base:        "https://opencode.ai/zen/go/v1/",
			api:         models.Openai,
			effort:      "high",
			explicit:    true,
			wantKey:     "explicit-fixture",
			wantSession: "conversation-fixture",
		},
		{
			name:     "Chat Completions streaming cache key",
			base:     "https://proxy.invalid/v1/",
			api:      models.Openai,
			effort:   "high",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Responses streaming cache key",
			base:     "https://proxy.invalid/v1/",
			api:      models.Responses,
			effort:   "high",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Anthropic streaming caching",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "high",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Responses default effort summary",
			base:     "https://proxy.invalid/v1/",
			api:      models.Responses,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Responses streaming default effort summary",
			base:     "https://proxy.invalid/v1/",
			api:      models.Responses,
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:    "ChatGPT default effort summary",
			base:    "https://chatgpt.com/backend-api/codex/",
			api:     models.Responses,
			wantKey: "access-fixture",
			stream:  true,
		},
		{
			name:     "Anthropic adaptive thinking summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "adaptive",
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Anthropic streaming adaptive thinking summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "adaptive",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Anthropic default effort summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Anthropic streaming default effort summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Anthropic max effort summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "max",
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Anthropic streaming max effort summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "max",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Anthropic xhigh effort summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "xhigh",
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Anthropic streaming xhigh effort summary",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "xhigh",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:     "Anthropic disabled thinking",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "disabled",
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "Anthropic streaming disabled thinking",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			effort:   "disabled",
			explicit: true,
			wantKey:  "explicit-fixture",
			stream:   true,
		},
		{
			name:        "Anthropic tool result caching",
			base:        "https://proxy.invalid/",
			api:         models.Anthropic,
			effort:      "high",
			explicit:    true,
			wantKey:     "explicit-fixture",
			toolResults: true,
		},
		{
			name:        "Anthropic streaming tool result caching",
			base:        "https://proxy.invalid/",
			api:         models.Anthropic,
			effort:      "high",
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
				wantSession := test.wantSession
				var toolResultPrefix string
				providers, providerErr := New(
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
							if got := request.Header.Get("X-Opencode-Session"); got != wantSession {
								t.Errorf(
									"OpenCode session: got %q, want %q",
									got,
									wantSession,
								)
							}
							if test.wantSession != "" && request.Header.Get("User-Agent") != "cpe/"+version.Get() {
								t.Errorf(
									"OpenCode user agent: %q",
									request.Header.Get("User-Agent"),
								)
							}
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
								ReasoningEffort string `json:"reasoning_effort"`
								Reasoning       struct {
									Effort  string `json:"effort"`
									Summary string `json:"summary"`
								} `json:"reasoning"`
								Thinking *struct {
									Type    string `json:"type"`
									Display string `json:"display"`
								} `json:"thinking"`
								OutputConfig struct {
									Effort *string `json:"effort"`
								} `json:"output_config"`
								PromptCacheKey string       `json:"prompt_cache_key"`
								System         []cacheBlock `json:"system"`
								Messages       []struct {
									Content json.RawMessage `json:"content"`
								} `json:"messages"`
							}
							if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if test.api == models.Openai && body.ReasoningEffort != string(test.effort) {
								t.Error("reasoning effort was not applied")
							}
							if test.api == models.Responses {
								if body.Reasoning.Effort != string(test.effort) || body.Reasoning.Summary != "auto" {
									t.Errorf(
										"reasoning configuration: %#v",
										body.Reasoning,
									)
								}
							}
							if test.api == models.Anthropic {
								switch test.effort {
								case "disabled":
									if body.Thinking == nil || body.Thinking.Type != "disabled" || body.Thinking.Display != "" {
										t.Errorf(
											"disabled thinking configuration: %#v",
											body.Thinking,
										)
									}
								default:
									if body.Thinking == nil || body.Thinking.Type != "adaptive" || body.Thinking.Display != "summarized" {
										t.Errorf(
											"thinking summary configuration: %#v",
											body.Thinking,
										)
									}
								}
								wantEffort := string(test.effort)
								if test.effort == "adaptive" || test.effort == "disabled" {
									wantEffort = ""
								}
								if wantEffort == "" {
									if body.OutputConfig.Effort != nil {
										t.Errorf(
											"unexpected explicit thinking effort: %q",
											*body.OutputConfig.Effort,
										)
									}
								} else if body.OutputConfig.Effort == nil {
									t.Error("missing explicit thinking effort")
								} else if *body.OutputConfig.Effort != wantEffort {
									t.Errorf(
										"thinking effort: got %q, want %q",
										*body.OutputConfig.Effort,
										wantEffort,
									)
								}
							}
							wantCacheKey := "conversation-fixture"
							if wantSession != "" {
								wantCacheKey = wantSession
							}
							if (test.api == models.Openai || test.api == models.Responses) && body.PromptCacheKey != wantCacheKey {
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
				if providerErr != nil {
					t.Fatal(providerErr)
				}
				t.Cleanup(providers.Close)
				base, providerErr := url.Parse(test.base)
				if providerErr != nil {
					t.Fatal(providerErr)
				}
				model := models.Model{
					ID:        "fixture",
					BaseURL:   *base,
					API:       test.api,
					MaxTokens: 64,
				}
				if test.explicit {
					secret := secrecy.NewSecretString([]byte("explicit-fixture"))
					model.APIKey = &secret
				}
				sessionID := conversation.ID("conversation-fixture")
				if test.noSession {
					sessionID = ""
				}
				generator, options, providerErr := providers.Generator(
					t.Context(),
					sessionID,
					model,
					test.effort,
				)
				if (providerErr != nil) != test.wantError {
					t.Fatalf(
						"generator: %v",
						providerErr,
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
				if test.wantSession != "" {
					// Another conversation must not change an existing generator's headers.
					other, _, err := providers.Generator(
						t.Context(),
						"other-conversation",
						model,
						test.effort,
					)
					if err != nil {
						t.Fatal(err)
					}
					if test.stream {
						other = &gai.StreamingAdapter{
							S: other.(gai.StreamingGenerator),
						}
					}
					wantSession = "other-conversation"
					otherRequest := request
					otherRequest.PromptCacheKey = wantSession
					if _, err := other.Generate(
						t.Context(),
						otherRequest,
					); err != nil {
						t.Fatal(err)
					}
					wantSession = test.wantSession
					if _, err := generator.Generate(
						t.Context(),
						request,
					); err != nil {
						t.Fatal(err)
					}
				}
				if err := providers.Logout(context.Background()); err != nil {
					t.Fatal(err)
				}
				remaining, providerErr := store.Retrieve(
					t.Context(),
					"/auth.json",
				)
				if providerErr != nil || remaining.OpenAI != nil || remaining.OpenCode != nil {
					t.Fatal("logout did not clear local credentials")
				}
			},
		)
	}
}
