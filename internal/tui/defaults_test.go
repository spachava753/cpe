package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gofrs/flock"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestDefaultSelection(t *testing.T) {
	for _, test := range []struct {
		name, command, value, wantModel, wantEffort, wantDefault, wantSavedEffort string
		cancel, badFile, badProvider, blocked, shutdown, afterCommit              bool
	}{
		{name: "save model", command: modelCommand, value: "beta", wantModel: "beta", wantEffort: "low", wantDefault: "beta", wantSavedEffort: "low"},
		{name: "save reasoning only", command: reasoningCommand, value: "high", wantModel: "beta", wantEffort: "high", wantDefault: "alpha", wantSavedEffort: "high"},
		{name: "provider reasoning", command: reasoningCommand, value: providerEffort, wantModel: "beta", wantDefault: "alpha"},
		{name: "explicit none", command: reasoningCommand, value: "none", wantModel: "beta", wantEffort: "none", wantDefault: "alpha", wantSavedEffort: "none"},
		{name: "cancel blocked save", command: modelCommand, value: "beta", blocked: true, wantModel: "alpha", wantDefault: "alpha", wantSavedEffort: "low"},
		{name: "shutdown blocked save", command: modelCommand, value: "beta", blocked: true, shutdown: true, wantModel: "alpha", wantDefault: "alpha", wantSavedEffort: "low"},
		{name: "cancel after commit", command: modelCommand, value: "beta", afterCommit: true, wantModel: "alpha", wantDefault: "beta", wantSavedEffort: "low"},
		{name: "cancel", command: modelCommand, value: "beta", cancel: true, wantModel: "alpha", wantDefault: "alpha", wantSavedEffort: "low"},
		{name: "save failure", command: modelCommand, value: "beta", badFile: true, wantModel: "alpha", wantDefault: "alpha", wantSavedEffort: "low"},
		{name: "provider failure", command: modelCommand, value: "beta", badProvider: true, wantModel: "alpha", wantDefault: "beta", wantSavedEffort: "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			profiles := map[string]config.Model{"alpha": {Provider: "codex", ID: "a"}, "beta": {Provider: "codex", ID: "b", ReasoningEffort: lowEffort}}
			data, err := json.Marshal(map[string]any{"default_model": "alpha", "models": profiles})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			gen := agenttest.NewScriptedGenerator()
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024}}, Model: profiles["alpha"], Generator: gen, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "alpha")
			m.profiles, m.configDir = profiles, dir
			started := make(chan struct{})
			m.newGenerator = func(ctx context.Context, _ config.Model) (gai.Generator, error) {
				if test.afterCommit {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if test.badProvider {
					return nil, errors.New("fixture provider unavailable")
				}
				return gen, nil
			}
			if test.command == reasoningCommand {
				m.configureModel(modelCommand, "beta")
			}
			// Ordinary commands only change the conversation; explicit provider omits effort.
			m.configureModel(reasoningCommand, "high")
			m.configureModel(reasoningCommand, providerEffort)
			m.configureModel(reasoningCommand, "")
			if m.picker.list.SelectedItem().(choice).value != providerEffort {
				t.Fatal("reasoning picker selected saved effort instead of active provider choice")
			}
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			if a.Model().ReasoningEffort != "" {
				t.Fatal("provider effort was not omitted")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || string(unchanged) != string(data) {
				t.Fatal("slash command wrote config", err)
			}
			m.input.SetValue("draft stays here")
			next, _ = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			m = next.(model)
			if m.picker == nil || m.picker.command != defaultsMenu {
				t.Fatal("missing defaults menu")
			}
			if test.command == reasoningCommand {
				m.picker.list.Select(1)
			}
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			if m.picker == nil || m.picker.scope != savedSelection {
				t.Fatal("missing save picker")
			}
			for i, item := range m.picker.list.Items() {
				if item.(choice).value == test.value {
					m.picker.list.Select(i)
				}
			}
			next, _ = m.Update(tea.PasteMsg{Content: "must not leak"})
			m = next.(model)
			if strings.Contains(m.View().Content, "must not leak") || m.input.Value() != "draft stays here" {
				t.Fatal("paste escaped picker")
			}
			if test.badFile {
				if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if test.blocked {
				lock := flock.New(path + ".lock")
				if err := lock.Lock(); err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}
			key := tea.KeyEnter
			if test.cancel {
				key = tea.KeyEsc
			}
			next, cmd := m.Update(tea.KeyPressMsg{Code: key})
			m = next.(model)
			if !test.cancel {
				if cmd == nil || m.savingDefault == nil {
					t.Fatal("save did not start")
				}
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
				if m.busy || len(a.Messages()) != 0 {
					t.Fatal("submitted while saving")
				}
				if test.afterCommit {
					<-started
				}
				if test.blocked || test.afterCommit {
					if test.shutdown {
						m.savingDefault.stop()
					} else {
						next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
						m = next.(model)
					}
					if m.input.Value() != "draft stays here" {
						t.Fatal("cancel erased draft")
					}
				}
				next, _ = m.Update(cmd())
				m = next.(model)
			}
			if m.name != test.wantModel || m.profile.ReasoningEffort != test.wantEffort || a.Model() != m.profile || m.savingDefault != nil {
				t.Fatalf("active=%s/%s notice=%s", m.name, m.profile.ReasoningEffort, m.notice)
			}
			if test.badFile {
				if !strings.HasPrefix(m.notice, "Error:") {
					t.Fatal(m.notice)
				}
				return
			}
			if (test.badProvider || test.afterCommit) && !strings.Contains(m.notice, "Default saved, but could not apply") {
				t.Fatal(m.notice)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var saved config.Config
			if err := json.Unmarshal(after, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.DefaultModel != test.wantDefault || saved.Models["beta"].ReasoningEffort != test.wantSavedEffort {
				t.Fatalf("saved=%+v", saved)
			}
			if m.input.Value() != "draft stays here" || len(a.Messages()) != 0 || len(store.Path()) != 1 {
				t.Fatal("selection changed draft or history")
			}
		})
	}
}
