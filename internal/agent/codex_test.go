package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/codex"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

type codexTestTransport func(*http.Request) (*http.Response, error)

func (f codexTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCodexAgentToolRoundTripAndCompaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data := fmt.Appendf(nil, `{"openai-codex":{"type":"oauth","access":"fixture-access","refresh":"fixture-refresh","expires":%d,"accountId":"fixture-account"}}`, time.Now().Add(time.Hour).UnixMilli())
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer fixture-access" || r.Header.Get("ChatGPT-Account-ID") != "fixture-account" {
			t.Error("wrong Codex endpoint or credentials")
		}
		if r.Header.Get("OpenAI-Beta") != "responses=experimental" || r.Header.Get("Originator") != "cpe" {
			t.Error("missing Codex protocol headers")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["stream"]) != "true" || string(body["store"]) != "false" || string(body["parallel_tool_calls"]) != "false" {
			t.Error("incorrect streaming or storage policy")
		}
		if _, ok := body["max_output_tokens"]; ok {
			t.Error("unsupported output limit sent")
		}
		if !strings.Contains(string(body["reasoning"]), `"low"`) {
			t.Error("missing reasoning effort")
		}
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			if !strings.Contains(string(body["tools"]), "starlark_repl") {
				t.Error("REPL tool absent")
			}
			fmt.Fprint(w, "data: "+`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc1","call_id":"call1","name":"starlark_repl","arguments":""}}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"code\":\"answer = 6 * 7; print(answer)\"}"}`+"\n\n")
		} else {
			if !strings.Contains(string(body["input"]), "42") {
				t.Error("missing durable tool result")
			}
			if requests == 3 && string(body["instructions"]) != `"Summarize for continuation"` {
				t.Error("compaction did not use its prompt")
			}
			fmt.Fprint(w, "data: "+`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg1","type":"message","role":"assistant","phase":"final_answer","content":[]}}`+"\n\n")
			fmt.Fprint(w, "data: "+`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"The answer is 42."}`+"\n\n")
		}
		// Exercise the Codex alias as well as the standard Responses event name.
		terminal := "response.completed"
		if requests == 3 {
			terminal = "response.done"
		}
		fmt.Fprintf(w, "data: {\"type\":%q,\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":4}}}\n\n", terminal)
	}))
	defer server.Close()
	originalTransport := http.DefaultTransport
	endpoint, err := url.Parse(server.URL + "/responses")
	if err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = codexTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
			return nil, fmt.Errorf("unexpected request destination")
		}
		clone := req.Clone(req.Context())
		clone.URL = endpoint
		clone.Host = endpoint.Host
		return originalTransport.RoundTrip(clone)
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	gen := codex.New(path)
	store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := Open(t.Context(), Options{
		Config: config.Config{System: "Calculate", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 4}, Compaction: config.Compaction{Prompt: "Summarize for continuation"}},
		Model:  config.Model{ID: "codex-fixture", ReasoningEffort: "low", MaxOutputTokens: 100}, Generator: gen, Store: store, CWD: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "Calculate 6 * 7", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("expected exactly 3 streaming requests, got %d", requests)
	}
	journal, err := os.ReadFile(store.Filename())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-access", "fixture-refresh", "fixture-account"} {
		if strings.Contains(string(journal), secret) {
			t.Fatal("OAuth credentials leaked into the session")
		}
	}
}
