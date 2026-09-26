package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestPromptDelivery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    config.PromptMode
		submit  config.SubmitKey
		promote []int
		cancel  bool
		failure bool
		late    bool
		inspect string
		want    []string
		calls   int32
	}{
		{name: "multiple FIFO prompts", want: []string{"wait", "first", "second", "third"}, calls: 4},
		{name: "steering after generation already completed", mode: config.PromptSteer, late: true, want: []string{"wait", "first", "second", "third"}, calls: 2},
		{name: "steering batch while settling", mode: config.PromptSteer, want: []string{"wait", "first", "second", "third"}, calls: 2},
		{name: "shift enter steering", mode: config.PromptSteer, submit: config.SubmitShiftEnter, want: []string{"wait", "first", "second", "third"}, calls: 2},
		{name: "promote middle prompt", promote: []int{1}, want: []string{"wait", "second", "first", "third"}, calls: 4},
		{name: "two promotions while settling", promote: []int{1, 1}, want: []string{"wait", "second", "third", "first"}, calls: 3},
		{name: "steering settlement failure remains visible", mode: config.PromptSteer, failure: true, want: []string{"wait", "first", "second", "third"}, calls: 2},
		{name: "cancel pending steering", mode: config.PromptSteer, cancel: true, want: []string{"wait"}, calls: 1},
		{name: "queue inspection holds until Escape", inspect: "escape", want: []string{"wait", "first", "second", "third"}, calls: 4},
		{name: "leaving queue for usage resumes FIFO", inspect: "usage", want: []string{"wait", "first", "second", "third"}, calls: 4},
		{name: "queue completion dismissal keeps hold", inspect: "completion", want: []string{"wait", "first", "second", "third"}, calls: 4},
		{name: "queue picker dismissal keeps hold", inspect: "picker", want: []string{"wait", "first", "second", "third"}, calls: 4},
		{name: "promote after active work finishes in queue", inspect: "promote", promote: []int{1}, want: []string{"wait", "second", "first", "third"}, calls: 4},
		{name: "promote from queue during active work", inspect: "promote busy", promote: []int{1}, want: []string{"wait", "second", "first", "third"}, calls: 4},
		{name: "idle queue submission joins held FIFO", inspect: "append", want: []string{"wait", "first", "second", "third", "fourth"}, calls: 5},
		{name: "explicit steering leaves held queue", inspect: "steer", want: []string{"wait", "direction", "first", "second", "third"}, calls: 5},
		{name: "clear held queue", inspect: "clear", want: []string{"wait"}, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			gen := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			opts := agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir}
			a, err := agent.Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "fixture")
			m.promptMode, m.submitKey = tc.mode, tc.submit
			m.start("wait")
			<-gen.started
			defer func() {
				if m.busy {
					m.cancel()
					for range m.events {
					}
				}
			}()
			next, _ := m.Update(update{event: agent.Event{Kind: agent.EventDelta, Text: "temporary streamed text"}})
			m = next.(model)
			var completed []update
			if tc.late {
				gen.finish <- nil
				for event := range m.events {
					completed = append(completed, event)
				}
			}
			key := tea.KeyPressMsg{Code: tea.KeyEnter}
			if tc.submit == config.SubmitShiftEnter {
				key.Mod = tea.ModShift
			}
			for _, text := range []string{"first", "second", "third"} {
				m.input.SetValue(text)
				next, _ = m.Update(key)
				m = next.(model)
				if m.input.Value() != "" {
					t.Fatal("submitted prompt retained in composer")
				}
			}
			if tc.mode == config.PromptQueue && len(m.queuedCommands) != 3 {
				t.Fatal("prompts not queued")
			}
			if tc.mode == config.PromptSteer && len(m.steering) != 3 {
				t.Fatal("steering prompts lost before settlement")
			}
			if tc.inspect != "" {
				m.input.SetValue(queueCommand)
				next, _ = m.Update(key)
				m = next.(model)
				if tc.inspect != "promote busy" {
					gen.finish <- nil
					deadline := time.After(5 * time.Second)
					for m.busy {
						select {
						case event := <-m.events:
							next, _ = m.Update(event)
							m = next.(model)
						case <-deadline:
							t.Fatal("active work did not settle during inspection")
						}
					}
					if gen.calls.Load() != 1 || len(m.queuedCommands) != 3 || m.panel.kind != queuePanel || !strings.Contains(m.View().Content, "Queue paused") {
						t.Fatal("inspection allowed automatic dispatch")
					}
					entries := len(store.Entries())
					next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
					m = next.(model)
					if tc.inspect == "completion" || tc.inspect == "picker" {
						if tc.inspect == "completion" {
							m.input.SetValue("/mo")
							m.syncCompletion()
						} else {
							m.input.SetValue(queueSteerCommand)
							next, _ = m.Update(key)
							m = next.(model)
						}
						next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
						m = next.(model)
					}
					if m.busy || len(m.queuedCommands) != 3 || m.panel.kind != queuePanel || len(store.Entries()) != entries {
						t.Fatal("layout or overlay dismissal released queued prompts")
					}
					switch tc.inspect {
					case "append", "steer", "clear":
						text := "fourth"
						switch tc.inspect {
						case "steer":
							m.promptMode = config.PromptSteer
							text = "direction"
						case "clear":
							text = "/queue clear"
						}
						m.input.SetValue(text)
						next, _ = m.Update(key)
						m = next.(model)
						if tc.inspect == "append" && (m.busy || len(m.queuedCommands) != 4 || len(store.Entries()) != entries) {
							t.Fatal("new prompt bypassed held FIFO")
						}
					}
					m.input.SetValue("keep this draft")
					if tc.inspect == "usage" {
						m.input.SetValue(usageCommand)
						next, _ = m.Update(key)
						m = next.(model)
					} else if tc.inspect != "promote" && tc.inspect != "steer" {
						next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
						m = next.(model)
					}
				}
			}
			for _, selection := range tc.promote {
				m.input.SetValue(queueSteerCommand)
				next, _ = m.Update(key)
				m = next.(model)
				if m.picker == nil {
					t.Fatal("missing promotion picker")
				}
				m.picker.list.Select(selection)
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
			}
			m.input.SetValue("keep this draft")
			if tc.cancel {
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = next.(model)
			}
			if tc.mode == config.PromptQueue && len(tc.promote) == 0 && tc.inspect == "" {
				gen.finish <- nil
			}
			for _, event := range completed {
				next, _ = m.Update(event)
				m = next.(model)
			}
			injectedFailure := false
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case event := <-m.events:
					if event.done && tc.failure && !injectedFailure {
						event.err = errors.Join(event.err, errors.New("fixture settlement failed"))
						injectedFailure = true
					}
					next, _ = m.Update(event)
					m = next.(model)
				case <-deadline:
					t.Fatal("work did not settle")
				}
			}
			if tc.failure && (len(m.operationErrors) != 1 || !strings.Contains(m.queueText(), "fixture settlement failed")) {
				t.Fatal("steering masked failure")
			}
			var users []string
			for _, msg := range a.Messages() {
				if msg.Role == gai.User {
					users = append(users, msg.Blocks[0].Content.String())
				}
			}
			if !reflect.DeepEqual(users, tc.want) || gen.calls.Load() != tc.calls || len(m.queuedCommands) != 0 || len(m.steering) != 0 || m.input.Value() != "keep this draft" {
				t.Fatalf("users=%v calls=%d queue=%v steering=%v draft=%q", users, gen.calls.Load(), m.queuedCommands, m.steering, m.input.Value())
			}
			if !tc.late && (tc.inspect == "" || tc.inspect == "promote busy") && (tc.mode == config.PromptSteer || len(tc.promote) > 0) {
				m.viewport.GotoTop()
				if m.interrupted == nil || !strings.Contains(m.View().Content, "interrupted by steering") {
					t.Fatal("interrupted stream lost without marker")
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"keep this draft", "temporary streamed text", queueSteerCommand} {
				if strings.Contains(string(data), private) {
					t.Fatal("provisional/UI text persisted", private)
				}
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			opts.Store = reopened
			restored, err := agent.Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if len(restored.Messages()) != len(a.Messages()) || gen.calls.Load() != tc.calls {
				t.Fatal("reopen replayed prompts")
			}
		})
	}
}

func TestPromptModeSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		save, busy bool
		value      string
	}{
		{name: "conversation picker", value: "steer"}, {name: "saved picker", save: true, value: "steer"}, {name: "save while generating", save: true, busy: true, value: "steer"}, {name: "explicit queue default", save: true, value: "queue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Config{Compaction: config.Compaction{Prompt: "Summarize"}, Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3, OutputLimit: 32000}, DefaultModel: "fixture", Models: map[string]config.Model{"fixture": {Provider: "codex", ID: "fixture"}}}
			data, err := json.Marshal(cfg)
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
			gate := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			var gen gai.Generator = agenttest.NewScriptedGenerator()
			if tc.busy {
				gen = gate
			}
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: cfg.Models["fixture"], Generator: gen, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "fixture")
			m.configDir = dir
			if tc.busy {
				m.start("wait")
				<-gate.started
				defer func() {
					if m.busy {
						m.cancel()
						for range m.events {
						}
					}
				}()
			}
			m.input.SetValue(promptModeCommand)
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			if m.picker == nil || m.picker.command != promptModeCommand {
				t.Fatal("missing mode picker")
			}
			if tc.value == "steer" {
				m.picker.list.Select(1)
			}
			key := tea.KeyPressMsg{Code: tea.KeyEnter}
			if tc.save {
				key = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
			}
			next, cmd := m.Update(key)
			m = next.(model)
			if tc.save {
				for _, msg := range commandMessages(cmd) {
					next, _ = m.Update(msg)
					m = next.(model)
				}
			}
			if m.promptMode.String() != tc.value {
				t.Fatalf("mode not applied: notice=%q saving=%v", m.notice, m.savingDefault != nil)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.save && string(after) != string(data) {
				t.Fatal("ordinary selection persisted")
			}
			if tc.save {
				var saved config.Config
				if err := json.Unmarshal(after, &saved); err != nil {
					t.Fatal(err)
				}
				if saved.TUI.PromptMode.String() != tc.value || saved.DefaultModel != "fixture" {
					t.Fatal("wrong default saved")
				}
			}
			m.input.SetValue("retained draft")
			if tc.busy {
				m.cancel()
				for m.busy {
					event := <-m.events
					next, _ = m.Update(event)
					m = next.(model)
				}
				if gate.calls.Load() != 1 {
					t.Fatal("setting change started generation")
				}
			}
			if m.input.Value() != "retained draft" {
				t.Fatal("lost draft")
			}
		})
	}
}
