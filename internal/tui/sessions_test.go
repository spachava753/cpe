package tui

import (
	"context"
	"errors"
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
)

func TestSessionCommands(t *testing.T) {
	for _, tc := range []struct {
		name       string
		busy, fail bool
	}{
		{name: "new"}, {name: "list and cancel"}, {name: "list and resume"}, {name: "resume current"}, {name: "failed new", fail: true}, {name: "new after active work", busy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Config{Dir: dir, DefaultModel: "alpha", Models: map[string]config.Model{"alpha": {ID: "a"}, "beta": {ID: "b"}}, Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}
			waiting := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			fail := false
			sessions := agent.NewSessions(agent.SessionOptions{Directory: filepath.Join(dir, "sessions"), CWD: dir, LoadConfig: func(context.Context) (config.Config, error) { return cfg, nil }, NewGenerator: func(context.Context, config.Model, string) (gai.Generator, error) {
				if fail {
					return nil, errors.New("fixture open failure")
				}
				if tc.busy {
					return waiting, nil
				}
				return agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("saved response")}}}, FinishReason: gai.EndTurn}}), nil
			}})
			defer sessions.Close()
			first, err := sessions.Open(t.Context(), agent.SessionStart{})
			if err != nil {
				t.Fatal(err)
			}
			firstPath := first.Agent.SessionFile()
			if !tc.busy {
				if err := first.Agent.Prompt(t.Context(), "first conversation", nil); err != nil {
					t.Fatal(err)
				}
			}
			m := newModel(t.Context(), first.Agent, first.Name)
			m.sessions, m.newGenerator, m.profiles = sessions, sessions.Generator, cfg.Models
			defer func() {
				if m.busy {
					m.cancel()
					for range m.events {
					}
				}
			}()
			if tc.busy {
				m.start("wait")
				<-waiting.started
			}
			cfg.DefaultModel = "beta"
			fail = tc.fail
			command := sessionNewCommand
			if tc.name == "list and cancel" || tc.name == "resume current" {
				command = sessionListCommand
			}
			m.input.SetValue(command)
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			m.input.SetValue("retained draft")
			if tc.busy {
				if len(m.queuedCommands) != 1 || m.agent != first.Agent {
					t.Fatal("session change bypassed active work")
				}
				waiting.finish <- nil
			}
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case msg := <-m.events:
					next, _ = m.Update(msg)
					m = next.(model)
				case <-deadline:
					t.Fatal("session operation did not settle")
				}
			}
			if tc.fail {
				if m.agent != first.Agent || !strings.Contains(m.notice, "fixture open failure") || len(m.operationErrors) != 1 {
					t.Fatal("failed session change lost old agent/error")
				}
			} else if command == sessionListCommand {
				if m.picker == nil || m.picker.command != sessionListCommand {
					t.Fatal("session list did not open picker")
				}
				key := tea.KeyPressMsg{Code: tea.KeyEsc}
				if tc.name == "resume current" {
					key.Code = tea.KeyEnter
				}
				next, _ = m.Update(key)
				m = next.(model)
				for m.busy {
					select {
					case msg := <-m.events:
						next, _ = m.Update(msg)
						m = next.(model)
					case <-deadline:
						t.Fatal("current resume did not settle")
					}
				}
				if m.agent != first.Agent || m.name != "alpha" {
					t.Fatal("cancel/current selection replaced conversation")
				}
			} else {
				if m.sessionFile == firstPath || len(m.messages) != 0 || m.name != "beta" || m.usage.Requests != 0 {
					t.Fatal("new conversation retained old state or missed saved default")
				}
				if tc.name == "list and resume" {
					m.input.SetValue(sessionListCommand)
					next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					m = next.(model)
					for m.busy {
						select {
						case msg := <-m.events:
							next, _ = m.Update(msg)
							m = next.(model)
						case <-deadline:
							t.Fatal("list did not settle")
						}
					}
					if m.picker == nil {
						t.Fatal("missing picker")
					}
					found := false
					for i, item := range m.picker.list.Items() {
						if item.(choice).value == firstPath {
							m.picker.list.Select(i)
							found = true
						}
					}
					if !found {
						t.Fatal("original session missing")
					}
					next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					m = next.(model)
					m.input.SetValue("retained draft")
					for m.busy {
						select {
						case msg := <-m.events:
							next, _ = m.Update(msg)
							m = next.(model)
						case <-deadline:
							t.Fatal("resume did not settle")
						}
					}
					if m.sessionFile != firstPath || len(m.messages) != 2 || m.messages[0].Blocks[0].Content.String() != "first conversation" || m.usage.Requests != 2 {
						t.Fatal("resumed wrong conversation/accounting")
					}
				}
			}
			if m.input.Value() != "retained draft" || m.picker != nil {
				t.Fatal("session operation lost draft or retained picker")
			}
		})
	}
	for _, name := range []string{"rename idle", "rename after first generation", "invalid rename"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Config{DefaultModel: "fixture", Models: map[string]config.Model{"fixture": {ID: "fixture"}}, Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}
			waiting := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			sessions := agent.NewSessions(agent.SessionOptions{Directory: filepath.Join(dir, "sessions"), CWD: dir, LoadConfig: func(context.Context) (config.Config, error) { return cfg, nil }, NewGenerator: func(context.Context, config.Model, string) (gai.Generator, error) { return waiting, nil }})
			defer sessions.Close()
			first, err := sessions.Open(t.Context(), agent.SessionStart{})
			if err != nil {
				t.Fatal(err)
			}
			m := newModel(t.Context(), first.Agent, first.Name)
			m.sessions = sessions
			next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m = next.(model)
			if name == "rename after first generation" {
				m.start("wait")
				<-waiting.started
			}
			title := "My Copilot investigation"
			if name == "invalid rename" {
				title = strings.Repeat("x", 121)
			}
			m.dispatchCommand(commandRequest{sessionRenameCommand, title})
			m.input.SetValue("keep draft")
			if name == "rename after first generation" {
				waiting.finish <- nil
			}
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case msg := <-m.events:
					next, _ = m.Update(msg)
					m = next.(model)
				case <-deadline:
					t.Fatal("rename did not finish")
				}
			}
			if name == "invalid rename" {
				if m.sessionTitle != "" || len(m.operationErrors) != 1 {
					t.Fatal("invalid rename changed title or lost error")
				}
				return
			}
			if m.sessionTitle != title || m.agent.Title().String() != title || !strings.Contains(m.View().Content, title) || m.sessionFile != first.Agent.SessionFile() {
				t.Fatal("rename missing from header/store or changed file")
			}
			if m.input.Value() != "keep draft" {
				t.Fatal("rename lost draft")
			}
			infos, err := sessions.List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			m.showSessions(infos)
			if m.picker == nil || !strings.Contains(m.picker.list.Items()[0].(choice).label, title) {
				t.Fatal("picker missing title")
			}
			for _, message := range m.messages {
				for _, block := range message.Blocks {
					if block.Content != nil && strings.Contains(block.Content.String(), title) {
						t.Fatal("rename entered dialog")
					}
				}
			}
		})
	}

	for _, action := range []string{"new before prompt", "resume before prompt", "consecutive new sessions"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Config{DefaultModel: "fixture", Models: map[string]config.Model{"fixture": {ID: "fixture"}}, Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}
			waiting := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			sessions := agent.NewSessions(agent.SessionOptions{Directory: filepath.Join(dir, "sessions"), CWD: dir,
				LoadConfig:   func(context.Context) (config.Config, error) { return cfg, nil },
				NewGenerator: func(context.Context, config.Model, string) (gai.Generator, error) { return waiting, nil },
			})
			defer sessions.Close()
			first, err := sessions.Open(t.Context(), agent.SessionStart{})
			if err != nil {
				t.Fatal(err)
			}
			target := ""
			if action == "resume before prompt" {
				target = first.Agent.SessionFile()
				first, err = sessions.Open(t.Context(), agent.SessionStart{})
				if err != nil {
					t.Fatal(err)
				}
			}
			oldPath := first.Agent.SessionFile()
			m := newModel(t.Context(), first.Agent, first.Name)
			m.sessions, m.newGenerator = sessions, sessions.Generator
			m.start("wait")
			<-waiting.started
			defer func() {
				if m.busy {
					m.cancel()
					for range m.events {
					}
				}
			}()
			request := commandRequest{kind: sessionNewCommand}
			if target != "" {
				request = commandRequest{sessionResumeCommand, target}
			}
			m.dispatchCommand(request)
			if action == "consecutive new sessions" {
				m.dispatchCommand(request)
			}
			m.input.SetValue("destination-only")
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			m.input.SetValue("private draft")
			waiting.finish <- nil
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case event := <-m.events:
					next, _ = m.Update(event)
					m = next.(model)
				case <-deadline:
					t.Fatal("queued session transitions did not settle")
				}
			}
			if m.sessionFile == oldPath || target != "" && m.sessionFile != target {
				t.Fatal("wrong destination", m.sessionFile)
			}
			if len(m.messages) != 2 || m.messages[0].Blocks[0].Content.String() != "destination-only" {
				t.Fatalf("wrong destination dialog: %+v", m.messages)
			}
			data, err := os.ReadFile(oldPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "destination-only") {
				t.Fatal("prompt was sent to old conversation")
			}
			found, err := sessions.List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			wantFiles := 2
			if action == "consecutive new sessions" {
				wantFiles = 3
			}
			if len(found) != wantFiles || len(m.queuedCommands) != 0 || waiting.calls.Load() != 2 || m.input.Value() != "private draft" {
				t.Fatal("lost transitions/order/draft", len(found), waiting.calls.Load())
			}
		})
	}

}
