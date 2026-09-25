package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/copilot"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestCopilotConversation(t *testing.T) {
	for _, test := range []struct {
		name, mode    string
		maxCharacters int
		messages      int
		initiators    []string
	}{
		{"generate", "generate", 0, 6, []string{"user", "agent", "agent", "agent"}},
		{"stream", "stream", 0, 6, []string{"user", "agent", "agent", "agent"}},
		{"generate compact before first response", "generate", 1, 6, []string{"agent", "user", "agent", "agent", "agent"}},
		{"stream compact before first response", "stream", 1, 6, []string{"agent", "user", "agent", "agent", "agent"}},
		{"generate compact after tool", "generate", 2000, 4, []string{"user", "agent", "agent", "agent", "agent"}},
		{"stream compact after tool", "stream", 2000, 4, []string{"user", "agent", "agent", "agent", "agent"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, ".cpe")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"default_model":"mine","models":{"mine":{"provider":"codex","id":"fixture"}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			requests, conversations := 0, 0
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = providerRoundTrip(func(r *http.Request) (*http.Response, error) {
				// Login and catalog discovery are local fixtures; the imported profile
				// is reloaded from disk before the actual Agent chooses its interface.
				var authBody string
				switch r.URL.String() {
				case "https://github.com/login/device/code":
					authBody = `{"device_code":"private-device","user_code":"CPE-TEST","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`
				case "https://github.com/login/oauth/access_token":
					authBody = `{"access_token":"github-secret","token_type":"bearer"}`
				case "https://api.github.com/copilot_internal/v2/token":
					authBody = fmt.Sprintf(`{"token":"copilot-secret","expires_at":%d,"endpoints":{"api":"https://api.githubcopilot.com"}}`, time.Now().Add(time.Hour).Unix())
				case "https://api.githubcopilot.com/models":
					authBody = fmt.Sprintf(`{"data":[{"id":"fixture","model_picker_enabled":true,"supported_endpoints":["/chat/completions"],"capabilities":{"supports":{"tool_calls":true,"streaming":%t},"limits":{"max_prompt_tokens":128000,"max_output_tokens":4000}}}]}`, test.mode == "stream")
				}
				if authBody != "" {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(authBody)), Request: r}, nil
				}
				requests++
				if r.URL.String() != "https://api.githubcopilot.com/chat/completions" || r.Header.Get("Authorization") != "Bearer copilot-secret" || r.Header.Get("X-Session-Id") != store.Entries()[0].ID {
					t.Error("Copilot route")
				}
				if requests > len(test.initiators) {
					t.Fatal("unexpected request")
				}
				if r.Header.Get("X-Initiator") != test.initiators[requests-1] {
					t.Errorf("initiator %q for request %d", r.Header.Get("X-Initiator"), requests)
				}
				var input struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Role    string `json:"role"`
						Content any    `json:"content"`
					} `json:"messages"`
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Fatal(err)
				}
				if input.Stream != (test.mode == "stream") {
					t.Fatal("wrong adapter mode")
				}
				summary := len(input.Tools) == 0
				if !summary {
					conversations++
				}
				if !summary && (len(input.Tools) != 1 || input.Tools[0].Function.Name != "starlark_repl") {
					t.Fatal("tool surface changed")
				}
				if !summary && (conversations == 2 || conversations == 3) {
					last := input.Messages[len(input.Messages)-1]
					want := "41"
					if conversations == 3 {
						want = "42"
					}
					role := "tool"
					if test.maxCharacters == 2000 && conversations == 2 {
						role, want = "user", "Conversation summary"
					}
					if last.Role != role || !strings.Contains(fmt.Sprint(last.Content), want) {
						t.Fatal("missing tool result")
					}
				}
				message := map[string]any{"role": "assistant", "content": "Done"}
				finish := "stop"
				if !summary && conversations <= 2 {
					code := `load("tools.star", "counter"); answer = counter(); print(answer)`
					if test.maxCharacters == 2000 {
						code += `; print("x" * 3000)`
					}
					if conversations == 2 {
						code = `print(answer + 1)`
					}
					args, _ := json.Marshal(map[string]string{"code": code})
					message = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call-%d", requests), "type": "function", "function": map[string]any{"name": "starlark_repl", "arguments": string(args)}}}}
					finish = "tool_calls"
				}
				field := "message"
				contentType := "application/json"
				if input.Stream {
					field = "delta"
					contentType = "text/event-stream"
				}
				body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, field: message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 5, "total_tokens": 25}})
				text := string(body)
				if input.Stream {
					text = "data: " + text + "\n\ndata: [DONE]\n\n"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(text)), Request: r}, nil
			})
			if _, err := copilot.Login(t.Context(), dir, nil); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			profile := loaded.Models["github-copilot/fixture"]
			if profile.CopilotStreaming != (test.mode == "stream") {
				t.Fatal("streaming capability lost on reload")
			}
			// Exercise character-triggered compaction separately from imported budgets.
			profile.ContextWindow = 0
			gen, err := Provider(t.Context(), profile, dir, store.Entries()[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			opts := Options{Config: config.Config{System: "fixture", Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 5, OutputLimit: 4000}, Compaction: config.Compaction{Prompt: "Summarize", MaxCharacters: test.maxCharacters}}, Model: profile, Generator: gen, Store: store, CWD: dir, Tools: []repl.Tool{{Name: "counter", Execute: func(context.Context, map[string]any) (any, error) { calls++; return 41, nil }}}}
			a, err := Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Prompt(t.Context(), "compute", nil); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || requests != len(test.initiators)-1 || conversations != 3 || len(a.Messages()) != test.messages {
				t.Fatalf("calls=%d requests=%d messages=%d", calls, requests, len(a.Messages()))
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			a, err = Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if calls != 1 || requests != len(test.initiators)-1 || conversations != 3 || len(a.Messages()) != test.messages {
				t.Fatal("reopen repeated effects or generation")
			}
			if err := a.Compact(t.Context()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || requests != len(test.initiators) || a.Usage().Requests != requests {
				t.Fatal("compaction/replay accounting changed")
			}
			data, err := os.ReadFile(store.Filename())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "copilot-secret") || strings.Contains(string(data), "github-secret") {
				t.Fatal("credential persisted in journal")
			}
		})
	}
}
