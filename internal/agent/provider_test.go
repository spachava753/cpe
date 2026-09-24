package agent

import (
	"encoding/json"
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
		for _, test := range []struct{ provider, path, authHeader, authValue string }{
			{"openai", "/zen/go/v1/chat/completions", "Authorization", "Bearer fixture-key"},
			{"responses", "/zen/go/v1/responses", "Authorization", "Bearer fixture-key"},
			{"anthropic", "/zen/go/v1/messages", "X-Api-Key", "fixture-key"},
		} {
			t.Run(test.provider, func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "opencode-go.json"), []byte(`{"key":"fixture-key"}`), 0600); err != nil {
					t.Fatal(err)
				}
				calls := 0
				previous := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = previous })
				http.DefaultTransport = providerRoundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.String() != "https://opencode.ai"+test.path {
						t.Errorf("endpoint %s", r.URL)
					}
					if r.Header.Get(test.authHeader) != test.authValue {
						t.Error("missing saved API key")
					}
					if r.Header.Get("X-Opencode-Session") != "durable-root" || !strings.HasPrefix(r.Header.Get("User-Agent"), "cpe/") {
						t.Error("missing CPE session identity")
					}
					return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture stops after capture","type":"fixture"}}`)), Request: r}, nil
				})
				profile := config.Model{Provider: test.provider, ID: "model", Credential: "opencode-go"}
				gen, err := Provider(t.Context(), profile, dir, "durable-root")
				if err != nil {
					t.Fatal(err)
				}
				request := gai.GenerationRequest{Model: "model", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}}}}
				if _, err := gen.Generate(t.Context(), request); err == nil {
					t.Fatal("expected fixture HTTP error")
				}
				var streamErr error
				for chunk := range gen.(gai.StreamingGenerator).Stream(t.Context(), request) {
					if chunk.Err != nil {
						streamErr = chunk.Err
					}
				}
				if streamErr == nil || calls != 2 {
					t.Fatalf("Generate/Stream failed before reaching Go or retried: calls=%d err=%v", calls, streamErr)
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
