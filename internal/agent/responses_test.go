package agent_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/responses"
	"github.com/spachava753/cpe/internal/session"
)

// Exercise the provider boundary through durable accounting, with both adapter
// interfaces. Explicit zero is complete; absent or null counters remain unknown.
func TestResponsesUsageAccounting(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			for _, test := range []struct {
				name, usage                string
				input, output, read, write int64
				unknown                    int
			}{
				{name: "empty", usage: `{}`, unknown: 1},
				{name: "null", usage: `null`, unknown: 1},
				{name: "input only", usage: `{"input_tokens":100}`, input: 100, unknown: 1},
				{name: "output only", usage: `{"output_tokens":20}`, output: 20, unknown: 1},
				{name: "null input", usage: `{"input_tokens":null,"output_tokens":20}`, output: 20, unknown: 1},
				{name: "absent input with caches", usage: `{"output_tokens":20,"input_tokens_details":{"cached_tokens":40,"cache_write_tokens":10}}`, output: 20, read: 40, write: 10, unknown: 1},
				{name: "null input with caches", usage: `{"input_tokens":null,"output_tokens":20,"input_tokens_details":{"cached_tokens":40,"cache_write_tokens":10}}`, output: 20, read: 40, write: 10, unknown: 1},
				{name: "explicit zero", usage: `{"input_tokens":0,"output_tokens":0}`},
				{name: "complete", usage: `{"input_tokens":100,"output_tokens":20}`, input: 100, output: 20},
			} {
				t.Run(test.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var request struct {
							Stream bool `json:"stream"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Error(err)
						}
						if request.Stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Done\",\"output_index\":0,\"content_index\":0}\n\n")
							fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":%s}}\n\n", test.usage)
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprintf(w, `{"id":"fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"}]}],"usage":%s}`, test.usage)
						}
					}))
					defer server.Close()
					client := openai.NewClient(option.WithAPIKey("fixture"), option.WithBaseURL(server.URL), option.WithMaxRetries(0))
					var generator gai.Generator = responses.New(&client.Responses)
					if mode == "generate" {
						generator = struct{ gai.Generator }{generator}
					}
					dir := t.TempDir()
					store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
					if err != nil {
						t.Fatal(err)
					}
					defer store.Close()
					rate := 1.0
					a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: config.Model{ID: "fixture", Cost: &config.Pricing{Rates: config.Rates{Input: &rate, Output: &rate, CacheRead: &rate, CacheWrite: &rate}}}, Generator: generator, Store: store, CWD: dir})
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = a.Close() }()
					if err := a.Prompt(t.Context(), "hello", nil); err != nil {
						t.Fatal(err)
					}
					got := a.Usage()
					if got.Input != test.input || got.Output != test.output || got.CacheRead != test.read || got.CacheWrite != test.write || got.Unreported != test.unknown || got.Unpriced != test.unknown || got.Requests != 1 {
						t.Fatalf("usage=%+v, want input=%d output=%d unknown=%d", got, test.input, test.output, test.unknown)
					}

					if err := a.Close(); err != nil {
						t.Fatal(err)
					}
					a, err = agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: config.Model{ID: "fixture"}, Generator: generator, Store: store, CWD: dir})
					if err != nil {
						t.Fatal(err)
					}
					if restored := a.Usage(); restored != got {
						t.Fatalf("reopen changed accounting: %+v, want %+v", restored, got)
					}
				})
			}
		})
	}
}
