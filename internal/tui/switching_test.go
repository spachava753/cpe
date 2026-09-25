package tui

import (
	"context"
	"errors"
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
)

func TestActiveModelSelection(t *testing.T) {
	for _, test := range []struct {
		name                                                         string
		picker, cancel, late, replace, providerError, shift, compact bool
	}{
		{name: "direct selection"}, {name: "picker during generation", picker: true},
		{name: "cancel preserves queued selection", cancel: true}, {name: "late completion notification", late: true},
		{name: "replace pending with current", replace: true}, {name: "provider setup failure", providerError: true},
		{name: "configured submit key", picker: true, shift: true}, {name: "compaction completion", compact: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "selection.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			profiles := map[string]config.Model{"alpha": {Provider: "responses", ID: "old", ReasoningEffort: lowEffort}, "beta": {Provider: "anthropic", ID: "new", ReasoningEffort: "high"}}
			old := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{System: "test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1000, MaxRounds: 3}, Compaction: config.Compaction{Prompt: "summarize"}}, Model: profiles["alpha"], Generator: old, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			// Compaction needs existing context but shares the same worker lifecycle.
			if test.compact {
				warmup := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("warmup")}}}, FinishReason: gai.EndTurn}})
				if err := a.SetModel(profiles["alpha"], warmup); err != nil {
					t.Fatal(err)
				}
				if err := a.Prompt(t.Context(), "initial", nil); err != nil {
					t.Fatal(err)
				}
				if err := a.SetModel(profiles["alpha"], old); err != nil {
					t.Fatal(err)
				}
			}
			m := newModel(t.Context(), a, "alpha")
			m.profiles = profiles
			nextGenerator := agenttest.NewScriptedGenerator()
			m.newGenerator = func(context.Context, config.Model) (gai.Generator, error) {
				if test.providerError {
					return nil, errors.New("fixture unavailable")
				}
				return nextGenerator, nil
			}
			submit := tea.KeyPressMsg{Code: tea.KeyEnter}
			if test.shift {
				m.submitKey = config.SubmitShiftEnter
				submit.Mod = tea.ModShift
			}
			text := "wait"
			if test.compact {
				text = compactCommand
			}
			m.start(text)
			defer func() {
				if m.busy {
					m.cancel()
					for u := range m.events {
						if u.done {
							break
						}
					}
				}
			}()
			select {
			case <-old.started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not start")
			}
			var completed *update
			if test.late {
				old.finish <- nil
				for {
					u := <-m.events
					if u.done {
						completed = &u
						break
					}
					next, _ := m.Update(u)
					m = next.(model)
				}
			}
			if test.picker {
				next, _ := m.Update(tea.KeyPressMsg{Text: "/mo"})
				m = next.(model)
				if len(m.completion.matches) != 1 || m.completion.matches[0].name != modelCommand || m.completionHeight() == 0 {
					t.Fatal("active completion must offer only model")
				}
				next, _ = m.Update(submit)
				m = next.(model)
				if m.picker == nil || !m.busy {
					t.Fatal("no active model picker")
				}
				next, _ = m.Update(tea.PasteMsg{Content: "private paste"})
				m = next.(model)
				if strings.Contains(m.View().Content, "private paste") || m.input.Value() != "" {
					t.Fatal("picker paste leaked")
				}
				m.picker.list.Select(1)
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
			} else {
				m.input.SetValue("/model beta")
				next, _ := m.Update(submit)
				m = next.(model)
			}
			if m.name != "alpha" || !m.busy {
				t.Fatal("selection interrupted active generation")
			}
			if test.providerError {
				if !strings.Contains(m.View().Content, "fixture unavailable") || m.pendingModel != "" {
					t.Fatal("failed selection hidden or queued")
				}
			} else if m.pendingModel != "beta" {
				t.Fatal("choice not queued")
			}
			if test.replace {
				m.input.SetValue("/model alpha")
				next, _ := m.Update(submit)
				m = next.(model)
			}
			m.input.SetValue("draft stays unsent")
			next, _ := m.Update(submit)
			m = next.(model)
			if test.cancel {
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = next.(model)
			} else if !test.late {
				old.finish <- nil
			}
			if completed != nil {
				next, _ = m.Update(*completed)
				m = next.(model)
			}
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case u := <-m.events:
					next, _ = m.Update(u)
					m = next.(model)
				case <-deadline:
					t.Fatal("worker never completed")
				}
			}
			want := "beta"
			if test.providerError || test.replace {
				want = "alpha"
			}
			if m.name != want || m.profile != profiles[want] || a.Model() != m.profile || m.pendingModel != "" || m.input.Value() != "draft stays unsent" {
				t.Fatalf("name=%s profile=%+v pending=%q draft=%q notice=%s", m.name, m.profile, m.pendingModel, m.input.Value(), m.notice)
			}
			if len(nextGenerator.Requests()) != 0 {
				t.Fatal("selection generated a new prompt")
			}
			for _, message := range a.Messages() {
				for _, block := range message.Blocks {
					if block.Content != nil && strings.Contains(block.Content.String(), "draft stays unsent") {
						t.Fatal("draft entered history")
					}
				}
			}
		})
	}
}
