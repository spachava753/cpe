package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

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
	gen, err := Provider(t.Context(), model)
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
