package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestModelHistoryRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name string
		a, b config.Model
	}{
		{"responses to anthropic", config.Model{Provider: "responses", ID: "a"}, config.Model{Provider: "anthropic", ID: "b"}},
		{"anthropic to gemini", config.Model{Provider: "anthropic", ID: "a"}, config.Model{Provider: "gemini", ID: "b"}},
		{"same provider different model", config.Model{Provider: "responses", ID: "a"}, config.Model{Provider: "responses", ID: "b"}},
		{"same model different endpoint", config.Model{Provider: "openai", ID: "same", BaseURL: "https://a.example"}, config.Model{Provider: "openai", ID: "same", BaseURL: "https://b.example"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "history.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			reply := func(marker string) gai.Response {
				return gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, ExtraFields: map[string]any{"wire_phase": marker}, Blocks: []gai.Block{
					{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str(marker + " thinking"), ExtraFields: map[string]any{"signature": marker}},
					{BlockType: gai.Content, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str("public reply"), ExtraFields: map[string]any{"content_signature": marker}},
				}}}, FinishReason: gai.EndTurn}
			}
			gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: reply("privateA")}, agenttest.GenerateStep{Response: reply("privateB")}, agenttest.GenerateStep{Response: reply("privateA2")}, agenttest.GenerateStep{Response: reply("privateB2")}, agenttest.GenerateStep{Response: reply("privateA3")})
			opts := Options{Config: config.Config{System: "Stable system", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1000, MaxRounds: 3}, Compaction: config.Compaction{Prompt: "Summarize"}}, Model: test.a, Generator: gen, Store: store, CWD: dir}
			a, err := Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = a.Close() }()
			for i, profile := range []config.Model{test.a, test.b, test.a, test.b} {
				if err := a.SetModel(profile, gen); err != nil {
					t.Fatal(err)
				}
				if err := a.Prompt(t.Context(), "continue", nil); err != nil {
					t.Fatal(err)
				}
				request := gen.Requests()[i]
				raw, _ := json.Marshal(request)
				want, unwanted := "privateA", "privateB"
				if i%2 == 1 {
					want, unwanted = unwanted, want
				}
				if strings.Contains(string(raw), unwanted) {
					t.Fatalf("foreign replay state sent: %s", raw)
				}
				if i > 1 && !strings.Contains(string(raw), want+" thinking") {
					t.Fatalf("own thinking lost: %s", raw)
				}
				if strings.Contains(string(raw), `"origin"`) {
					t.Fatal("CPE origin leaked to provider")
				}
			}
			canonical, _ := json.Marshal(a.dialog)
			if !strings.Contains(string(canonical), "privateA") || !strings.Contains(string(canonical), "privateB") {
				t.Fatal("projection mutated original history")
			}
			checkpoint := a.Checkpoints()[1].ID
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			opts.Model = test.a
			a, err = Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			restored, _ := json.Marshal(a.dialog)
			if string(canonical) != string(restored) {
				t.Fatal("restart lost original replay fields")
			}
			if err := a.Prompt(t.Context(), "after restart", nil); err != nil {
				t.Fatal(err)
			}
			request, _ := json.Marshal(gen.Requests()[4])
			if !strings.Contains(string(request), "privateA2") || strings.Contains(string(request), "privateB") {
				t.Fatalf("restart projection=%s", request)
			}
			if err := a.Branch(t.Context(), checkpoint); err != nil {
				t.Fatal(err)
			}
			request, _ = json.Marshal(a.conversationRequest(a.dialog))
			if !strings.Contains(string(request), "privateA thinking") || strings.Contains(string(request), "privateA2") {
				t.Fatalf("branch origins misaligned: %s", request)
			}
		})
	}
}

func TestAgentProjectRequest(t *testing.T) {
	for _, test := range []struct {
		name                                                     string
		editSystem, editTools, editHistory, legacy, restoreChain bool
	}{
		{name: "unchanged prefix"}, {name: "changed system", editSystem: true}, {name: "changed tools", editTools: true},
		{name: "changed earlier message", editHistory: true}, {name: "legacy unknown origin", legacy: true},
		{name: "restoring earlier thinking invalidates later chain", restoreChain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := config.Model{Provider: "anthropic", ID: "claude-opus-5-5"}
			a := &Agent{opts: Options{Model: model, Config: config.Config{System: "Original"}}, origins: make(map[int]messageOrigin)}
			a.dialog = gai.Dialog{gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("original user")}}}
			add := func(marker string) {
				req := a.conversationRequest(a.dialog)
				origin, err := a.originFor(req)
				if err != nil {
					t.Fatal(err)
				}
				a.origins[len(a.dialog)] = *origin
				a.dialog = append(a.dialog, gai.Message{Role: gai.Assistant, Blocks: []gai.Block{
					{BlockType: gai.Thinking, ModalityType: gai.Text, MimeType: "text/plain", Content: gai.Str(marker)}, gai.TextBlock("public"),
				}})
			}
			add("first secret")
			if test.editSystem || test.restoreChain {
				a.opts.Config.System = "Changed"
			}
			if test.editTools {
				a.opts.Tools = []repl.Tool{{Name: "new_tool", Description: "new instructions"}}
			}
			if test.editHistory {
				a.dialog[0] = gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("edited user")}}
			}
			if test.legacy {
				clear(a.origins)
			}
			if test.restoreChain {
				a.dialog = append(a.dialog, gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("next")}})
				add("second secret")
				a.opts.Config.System = "Original"
			}
			before, _ := json.Marshal(a.dialog)
			projected, _ := json.Marshal(a.conversationRequest(a.dialog))
			wantFirst := !test.editSystem && !test.editTools && !test.editHistory && !test.legacy
			if strings.Contains(string(projected), "first secret") != wantFirst || strings.Contains(string(projected), "second secret") {
				t.Fatalf("projection=%s", projected)
			}
			after, _ := json.Marshal(a.dialog)
			if string(before) != string(after) {
				t.Fatal("projection changed canonical dialog")
			}
		})
	}
}
