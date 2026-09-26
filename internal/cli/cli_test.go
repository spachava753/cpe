package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/cli"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		name   string
		resume bool
		branch bool
	}{{name: "ordinary startup always creates new session"}, {name: "explicit resume", resume: true}, {name: "earlier branch excludes unavailable tool", branch: true}} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CPE_TEST_KEY", "fixture")
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			dialogSizes := make(chan int, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []json.RawMessage `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if len(req.Messages) == 0 || !strings.Contains(string(req.Messages[0]), "Generate a short session title") {
					dialogSizes <- len(req.Messages)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"id\":\"fixture\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			dir := filepath.Join(home, ".cpe")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{DefaultModel: "fixture", Models: map[string]config.Model{"fixture": {Provider: "openai", ID: "fixture", BaseURL: server.URL, APIKeyEnv: "CPE_TEST_KEY"}}, Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}, Compaction: config.Compaction{Prompt: "Summarize"}}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			if tc.branch {
				path := filepath.Join(dir, "sessions", "branch.jsonl")
				store, err := session.Open(path, cwd, repl.Runtime)
				if err != nil {
					t.Fatal(err)
				}
				root := store.Entries()[0].ID
				call, err := gai.ToolCallBlock("seed", "starlark_repl", map[string]any{"code": `load("tools.star", "counter"); counter()`})
				if err != nil {
					t.Fatal(err)
				}
				gen := agenttest.NewScriptedGenerator(
					agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
					agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}}, FinishReason: gai.EndTurn}},
				)
				effects := 0
				a, err := agent.Open(t.Context(), agent.Options{Config: cfg, Model: cfg.Models["fixture"], Store: store, CWD: cwd, Generator: gen, Tools: []repl.Tool{{Name: "counter", Execute: func(context.Context, map[string]any) (any, error) { effects++; return 42, nil }}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := a.Prompt(t.Context(), "abandoned future", nil); err != nil {
					t.Fatal(err)
				}
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				if err := cli.Run(t.Context(), []string{"--resume", path, "--branch", root, "--prompt", "safe earlier checkpoint"}, &out, &errOut); err != nil {
					t.Fatal(err)
				}
				if effects != 1 || !strings.Contains(out.String(), "done") || len(dialogSizes) != 1 {
					t.Fatal("branch repeated effects or missed generation", effects)
				}
				return
			}
			if err := cli.Run(context.Background(), []string{"--prompt", "first"}, &out, &errOut); err != nil {
				t.Fatal(err)
			}
			first, err := session.List(t.Context(), filepath.Join(dir, "sessions"), cwd)
			if err != nil || len(first) != 1 || first[0].Title.String() != "done" {
				t.Fatal(first, err)
			}
			args := []string{"--prompt", "second"}
			if tc.resume {
				args = append(args, "--resume", first[0].Path)
			}
			if err := cli.Run(t.Context(), args, &out, &errOut); err != nil {
				t.Fatal(err)
			}
			all, err := session.List(t.Context(), filepath.Join(dir, "sessions"), cwd)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			dialogs := []int{<-dialogSizes, <-dialogSizes}
			if tc.resume {
				want = 1
			}
			if len(all) != want || len(dialogs) != 2 {
				t.Fatalf("sessions=%d requests=%v", len(all), dialogs)
			}
			if (dialogs[1] > dialogs[0]) != tc.resume {
				t.Fatal("incorrect startup history", dialogs)
			}
			if !strings.Contains(out.String(), "done") {
				t.Fatal(out.String())
			}
		})
	}
}
