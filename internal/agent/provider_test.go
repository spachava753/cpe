package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestProvider(t *testing.T) {
	t.Run("OpenCode Go routing", func(t *testing.T) {
		for _, test := range []struct{ provider, path, authHeader, authValue, response, stream string }{
			{"openai", "/zen/go/v1/chat/completions", "Authorization", "Bearer fixture-key",
				`{"choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`,
				"data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"},
			{"responses", "/zen/go/v1/responses", "Authorization", "Bearer fixture-key",
				`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}]}`,
				"data: " + `{"type":"response.output_text.delta","delta":"Hello","output_index":0,"content_index":0}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed"}}` + "\n\n"},
			{"anthropic", "/zen/go/v1/messages", "X-Api-Key", "fixture-key",
				`{"id":"fixture","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
				"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` +
					"\n\nevent: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` +
					"\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` +
					"\n\nevent: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` +
					"\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` +
					"\n\nevent: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"},
		} {
			t.Run(test.provider, func(t *testing.T) {
				for _, mode := range []string{"generate", "stream"} {
					t.Run(mode, func(t *testing.T) {
						dir := t.TempDir()
						path := filepath.Join(dir, "auth.json")
						if err := os.WriteFile(path, []byte(`{"opencode-go":{"key":"rejected-key"}}`), 0600); err != nil {
							t.Fatal(err)
						}
						t.Setenv("OPENAI_API_KEY", "unrelated-openai-key")
						t.Setenv("ANTHROPIC_API_KEY", "unrelated-anthropic-key")
						calls := 0
						previous := http.DefaultTransport
						t.Cleanup(func() { http.DefaultTransport = previous })
						http.DefaultTransport = providerRoundTrip(func(r *http.Request) (*http.Response, error) {
							calls++
							if r.URL.String() != "https://opencode.ai"+test.path {
								t.Errorf("endpoint %s", r.URL)
							}
							want := test.authValue
							if calls == 1 {
								want = strings.ReplaceAll(want, "fixture-key", "rejected-key")
							}
							if r.Header.Get(test.authHeader) != want {
								t.Error("missing saved API key")
							}
							if r.Header.Get("X-Opencode-Session") != "durable-root" || !strings.HasPrefix(r.Header.Get("User-Agent"), "cpe/") {
								t.Error("missing CPE session identity")
							}
							body, err := io.ReadAll(r.Body)
							if err != nil {
								t.Fatal(err)
							}
							if strings.Contains(string(body), "-key") {
								t.Error("credentials in request body")
							}
							status, contentType, response := 200, "application/json", test.response
							if mode == "stream" {
								contentType, response = "text/event-stream", test.stream
							}
							if calls == 1 {
								status, contentType, response = 401, "application/json", `{"error":{"message":"Invalid API key","type":"authentication_error"}}`
							}
							return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
						})
						profile := config.Model{Provider: test.provider, ID: "fixture", Credential: "opencode-go"}
						gen, err := Provider(t.Context(), profile, dir, "durable-root")
						if err != nil {
							t.Fatal(err)
						}
						request := gai.GenerationRequest{Model: "fixture", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}}}, Options: gai.NewGenerationOptions(gai.WithMaxGenerationTokens(100))}
						invoke := gen
						if mode == "stream" {
							invoke = &gai.StreamingAdapter{S: gen.(gai.StreamingGenerator)}
						}
						_, err = invoke.Generate(t.Context(), request)
						var apiErr *gai.ApiErr
						if !errors.As(err, &apiErr) || apiErr.Provider != "opencode-go" || apiErr.StatusCode != 401 || apiErr.Kind != gai.APIErrorKindAuthentication || apiErr.Retryable() || !strings.Contains(err.Error(), "/login opencode-go") || calls != 1 {
							t.Fatalf("authentication error: %v (calls=%d)", err, calls)
						}
						// Replacing the key recovers on the same generator, without a restart.
						if err := os.WriteFile(path, []byte(`{"opencode-go":{"key":"fixture-key"}}`), 0600); err != nil {
							t.Fatal(err)
						}
						response, err := invoke.Generate(t.Context(), request)
						if err != nil || calls != 2 || len(response.Candidates) != 1 || len(response.Candidates[0].Blocks) != 1 || response.Candidates[0].Blocks[0].Content.String() != "Hello" {
							t.Fatalf("credential recovery: response=%+v err=%v calls=%d", response, err, calls)
						}
					})
				}
			})
		}
	})

	t.Run("switched tool history", func(t *testing.T) {
		for _, provider := range []string{"openai", "responses", "anthropic", "gemini"} {
			t.Run(provider, func(t *testing.T) {
				t.Setenv("CPE_SWITCH_FIXTURE_KEY", "fixture")
				var bodies []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					bodies = append(bodies, string(data))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"message":"fixture captured history","type":"invalid_request_error","status":"INVALID_ARGUMENT"}}`)
				}))
				defer server.Close()
				profile := config.Model{Provider: provider, ID: "fixture", APIKeyEnv: "CPE_SWITCH_FIXTURE_KEY", BaseURL: server.URL, MaxOutputTokens: 4096}
				gen, err := Provider(t.Context(), profile, t.TempDir(), "fixture")
				if err != nil {
					t.Fatal(err)
				}
				call, err := gai.ToolCallBlock("foreign-call", "starlark_repl", map[string]any{codeParameter: "print(42)"})
				if err != nil {
					t.Fatal(err)
				}
				call.ExtraFields = map[string]any{gai.GeminiExtraFieldThoughtSignature: "foreign-signature"}
				a := &Agent{opts: Options{Model: profile}, dialog: gai.Dialog{
					{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("compute")}},
					{Role: gai.Assistant, ExtraFields: map[string]any{gai.OpenAIExtraFieldWireFields: map[string]any{"reasoning_content": "foreign-secret"}}, Blocks: []gai.Block{{BlockType: gai.Thinking, ModalityType: gai.Text, Content: gai.Str("foreign-secret"), ExtraFields: map[string]any{gai.AnthropicExtraFieldThinkingSignature: "foreign-signature"}}, call}},
					gai.ToolResultMessage("foreign-call", gai.TextBlock("42"), gai.ImageBlock([]byte{1, 2, 3}, "image/png")),
				}}
				before, _ := json.Marshal(a.dialog)
				req := a.conversationRequest(a.dialog)
				if _, err := gen.Generate(t.Context(), req); err == nil {
					t.Fatal("expected HTTP fixture error")
				}
				for range gen.(gai.StreamingGenerator).Stream(t.Context(), req) {
				}
				if len(bodies) != 2 {
					t.Fatalf("provider rejected transferable history before HTTP: bodies=%v", bodies)
				}
				for _, body := range bodies {
					if strings.Contains(body, "foreign-secret") || strings.Contains(body, "foreign-signature") || !strings.Contains(body, "starlark_repl") || !strings.Contains(body, "42") || !strings.Contains(body, "image/png") {
						t.Fatalf("wire history=%s", body)
					}
				}
				after, _ := json.Marshal(a.dialog)
				if string(before) != string(after) {
					t.Fatal("provider call changed canonical history")
				}
			})
		}
	})

	type field struct {
		path  []string
		value any
	}
	for _, test := range []struct {
		name, provider, effort string
		fields                 []field
	}{
		{"compatible chat", "openai", "high", []field{{[]string{"reasoning_effort"}, "high"}}},
		{"compatible chat omitted", "openai", "", []field{{[]string{"reasoning_effort"}, nil}}},
		{"responses", "responses", "high", []field{{[]string{"reasoning", "effort"}, "high"}}},
		{"responses omitted", "responses", "", []field{{[]string{"reasoning", "effort"}, nil}}},
		{"anthropic effort", "anthropic", "high", []field{{[]string{"thinking", "type"}, "adaptive"}, {[]string{"output_config", "effort"}, "high"}}},
		{"anthropic adaptive", "anthropic", "adaptive", []field{{[]string{"thinking", "type"}, "adaptive"}, {[]string{"output_config", "effort"}, nil}}},
		{"anthropic disabled", "anthropic", "disabled", []field{{[]string{"thinking", "type"}, "disabled"}, {[]string{"output_config", "effort"}, nil}}},
		{"anthropic omitted", "anthropic", "", []field{{[]string{"thinking"}, nil}, {[]string{"output_config", "effort"}, nil}}},
		{"gemini", "gemini", "high", []field{{[]string{"generationConfig", "thinkingConfig", "thinkingLevel"}, "high"}}},
		{"gemini omitted", "gemini", "", []field{{[]string{"generationConfig", "thinkingConfig", "thinkingLevel"}, nil}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, mode := range []string{"generate", "stream"} {
				t.Run(mode, func(t *testing.T) {
					captured := make(chan map[string]any, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						captured <- body
						// Stop after capture: this tests request construction without
						// duplicating each provider's response or streaming protocol.
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						fmt.Fprint(w, `{"error":{"code":400,"message":"fixture stops after capture","type":"fixture"}}`)
					}))
					defer server.Close()
					t.Setenv("CPE_REASONING_TEST_KEY", "fixture-key")
					profile := config.Model{Provider: test.provider, ID: "custom-model", APIKeyEnv: "CPE_REASONING_TEST_KEY", BaseURL: server.URL}
					profile, err := profile.WithReasoningEffort(test.effort)
					if err != nil {
						t.Fatal(err)
					}
					generator, err := Provider(t.Context(), profile, t.TempDir(), "fixture-session")
					if err != nil {
						t.Fatal(err)
					}
					a := &Agent{opts: Options{Model: profile}}
					request := gai.GenerationRequest{Model: profile.ID, Options: a.generationOptions(), Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}}}}
					if mode == "stream" {
						for chunk := range generator.(gai.StreamingGenerator).Stream(t.Context(), request) {
							if chunk.Err != nil {
								err = chunk.Err
							}
						}
					} else {
						_, err = generator.Generate(t.Context(), request)
					}
					if err == nil {
						t.Fatal("expected fixture HTTP error")
					}
					select {
					case body := <-captured:
						for _, expected := range test.fields {
							var got any = body
							for _, key := range expected.path {
								object, ok := got.(map[string]any)
								if !ok {
									got = nil
									break
								}
								got = object[key]
							}
							if got != expected.value {
								t.Errorf("%s = %v, want %v; body=%v", strings.Join(expected.path, "."), got, expected.value, body)
							}
						}
					default:
						t.Fatalf("reasoning rejected before the provider request: %v", err)
					}
				})
			}
		})
	}
}

func TestOpenAICompatibleStreamingProvider(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing configured credentials")
		}
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Tools  []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if req.Model != "fixture-model" || !req.Stream || len(req.Tools) != 1 || req.Tools[0].Function.Name != "starlark_repl" {
			t.Errorf("bad request: %+v", req)
		}
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			fmt.Fprint(w, "data: "+`{"id":"r1","object":"chat.completion.chunk","model":"fixture-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"http-call","type":"function","function":{"name":"starlark_repl","arguments":"{\"code\":\"value = 6 * 7; print(value)\"}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"id":"r1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || !strings.Contains(fmt.Sprint(last.Content), "42") {
				t.Errorf("missing tool result: %+v", last)
			}
			fmt.Fprint(w, "data: "+`{"id":"r2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"The answer is 42."},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"id":"r2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("CPE_TEST_API_KEY", "fixture-key")
	model := config.Model{Provider: "openai", ID: "fixture-model", APIKeyEnv: "CPE_TEST_API_KEY", BaseURL: server.URL + "/v1"}
	gen, err := Provider(t.Context(), model, t.TempDir(), "fixture-session")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := Open(t.Context(), Options{Config: config.Config{System: "Test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 5}}, Model: model, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var provisional strings.Builder
	if err := a.Prompt(t.Context(), "calculate", func(e Event) {
		if e.Kind == EventDelta {
			provisional.WriteString(e.Text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(a.Messages()) != 4 || provisional.String() != "The answer is 42." {
		t.Fatalf("requests=%d messages=%d provisional=%q", requests, len(a.Messages()), provisional.String())
	}
}

// The adapter constructs real HTTP requests; this transport keeps them offline.
type providerRoundTrip func(*http.Request) (*http.Response, error)

func (f providerRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
