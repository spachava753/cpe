package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/skills"
)

func TestSkillSlashCommands(t *testing.T) {
	for _, tc := range []struct {
		name, draft, args, wantPrompt, wantError string
		key                                      rune
	}{
		{name: "tab then arguments", draft: "/skill:re", key: tea.KeyTab, args: "staged changes", wantPrompt: "/skill:review staged changes"},
		{name: "enter runs user-only skill", draft: "/skill:pu", key: tea.KeyEnter, wantPrompt: "/skill:publish"},
		{name: "escape preserves recognized command", draft: "/skill:review", key: tea.KeyEsc, wantPrompt: "/skill:review"},
		{name: "model-only skill rejected", draft: "/skill:background", key: tea.KeyEnter, wantError: "not user-invocable"},
		{name: "unknown skill rejected", draft: "/skill:missing", key: tea.KeyEnter, wantError: "unknown skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "skills")
			for _, fixture := range []struct{ name, flags string }{
				{"review", ""}, {"publish", "disable-model-invocation: true\n"}, {"background", "user-invocable: false\n"},
			} {
				skillDir := filepath.Join(root, fixture.name)
				if err := os.MkdirAll(skillDir, 0700); err != nil {
					t.Fatal(err)
				}
				data := "---\nname: " + fixture.name + "\ndescription: \"Review\\n\\e[31mcode\\e[0m\"\n" + fixture.flags + "---\nRead this when invoked."
				if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			catalog, warnings := skills.Discover(root)
			if len(warnings) != 0 {
				t.Fatal(warnings)
			}
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Skill accepted")}}}, FinishReason: gai.EndTurn}})
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir, Skills: catalog})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "skills-fixture")
			next, _ := m.Update(tea.KeyPressMsg{Text: "/skill:"})
			m = next.(model)
			if len(m.completion.matches) != 2 || m.completion.matches[0].name != "/skill:publish" || m.completion.matches[1].name != "/skill:review" {
				t.Fatalf("unexpected skill suggestions: %+v", m.completion.matches)
			}
			if m.completion.matches[1].description != "Review code" {
				t.Fatalf("description was not sanitized: %q", m.completion.matches[1].description)
			}
			m.input.SetValue(tc.draft)
			m.syncCompletion()
			before := len(store.Entries())
			next, _ = m.Update(tea.KeyPressMsg{Code: tc.key})
			m = next.(model)
			if tc.key != tea.KeyEnter {
				if m.busy || m.completionHeight() != 0 {
					t.Fatal("Tab/Escape executed a skill or left completion open")
				}
				if tc.args != "" {
					next, _ = m.Update(tea.KeyPressMsg{Text: tc.args})
					m = next.(model)
					if m.input.Value() != tc.wantPrompt {
						t.Fatalf("completed draft = %q", m.input.Value())
					}
				}
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
			}
			if !m.busy {
				t.Fatalf("skill command was not routed to agent: %s", m.notice)
			}
			defer m.cancel()
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case event := <-m.events:
					next, _ = m.Update(event)
					m = next.(model)
				case <-deadline:
					t.Fatal("skill command did not complete")
				}
			}
			if tc.wantError != "" {
				if !strings.Contains(m.notice, tc.wantError) || len(a.Messages()) != 0 || len(store.Entries()) != before {
					t.Fatalf("rejected command notice = %q, messages = %+v", m.notice, a.Messages())
				}
				return
			}
			if len(a.Messages()) != 2 || !strings.HasPrefix(a.Messages()[0].Blocks[0].Content.String(), tc.wantPrompt+"\n\n") || m.input.Value() != "" || m.completion.dismissed {
				t.Fatalf("skill prompt did not complete cleanly: messages=%+v, notice=%s", a.Messages(), m.notice)
			}
		})
	}
}
