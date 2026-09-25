package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestOpenCodeGoLogin(t *testing.T) {
	for _, test := range []struct {
		name, action string
		picker       bool
		submitKey    config.SubmitKey
	}{
		{name: "direct login", action: "save"},
		{name: "shift submit preference", action: "save", picker: true, submitKey: config.SubmitShiftEnter},
		{name: "provider picker", action: "save", picker: true},
		{name: "cancel pasted provider picker", action: "picker cancel", picker: true},
		{name: "escape at input", action: "esc"},
		{name: "control C at input", action: "ctrl+c"},
		{name: "cancel during import", action: "cancel"},
		{name: "failed import", action: "fail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "login.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			profile := config.Model{Provider: "codex", ID: "fixture"}
			goProfile := config.Model{Provider: "anthropic", ID: "go-model", Credential: "opencode-go"}
			gen := agenttest.NewScriptedGenerator()
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}}, Model: profile, Generator: gen, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "codex")
			m.submitKey = test.submitKey
			m.profiles = map[string]config.Model{"codex": profile}
			m.loginRequired = true
			m.needsLogin = func(p config.Model) bool { return p.Provider == "codex" }
			m.newGenerator = func(context.Context, config.Model) (gai.Generator, error) { return gen, nil }
			m.login = func(context.Context, string, func(string)) error { return errors.New("wrong login provider") }
			called := make(chan string, 1)
			m.loginGo = func(ctx context.Context, key string) (map[string]config.Model, error) {
				called <- key
				if test.action == "cancel" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				if test.action == "fail" {
					return nil, errors.New("catalog unavailable")
				}
				return map[string]config.Model{"codex": profile, "opencode-go/fixture": goProfile}, nil
			}
			command := loginGoCommand
			if test.picker {
				command = loginCommand
			}
			m.input.SetValue(command)
			submit := tea.KeyPressMsg{Code: tea.KeyEnter}
			if test.submitKey == config.SubmitShiftEnter {
				submit.Mod = tea.ModShift
			}
			next, _ := m.Update(submit)
			m = next.(model)
			if test.picker {
				if m.picker == nil || m.picker.command != loginCommand {
					t.Fatal("missing login picker")
				}
				next, _ = m.Update(tea.PasteMsg{Content: "private chooser paste"})
				m = next.(model)
				if m.input.Value() != "" || strings.Contains(m.View().Content, "private chooser paste") || len(a.Messages()) != 0 || len(store.Path()) != 1 {
					t.Fatal("login chooser paste escaped into the composer, display, or journal")
				}
				if test.action == "picker cancel" {
					next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
					m = next.(model)
					if m.picker != nil || m.input.Value() != "" || strings.Contains(m.View().Content, "private chooser paste") {
						t.Fatal("cancel retained pasted credentials")
					}
					return
				}
				for range 2 {
					next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
					m = next.(model)
				}
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
			}
			if m.keyInput == nil || !m.loggingIn || m.busy || m.picker != nil {
				t.Fatal("missing private key input")
			}
			const secret = "sensitive-fixture-key"
			next, _ = m.Update(tea.PasteMsg{Content: secret})
			m = next.(model)
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
			m = next.(model)
			if m.busy || m.keyInput == nil || m.input.Value() != "" || m.keyInput.Value() != secret {
				t.Fatal("key entered composer instead of private input")
			}
			for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 28}, {Width: 32, Height: 12}, {Width: 24, Height: 8}} {
				next, _ = m.Update(size)
				m = next.(model)
				view := m.View().Content
				if strings.Contains(view, secret) {
					t.Fatal("API key visible")
				}
				for line := range strings.SplitSeq(view, "\n") {
					if ansi.StringWidth(line) > size.Width {
						t.Fatalf("login exceeds width: %q", line)
					}
				}
				if strings.Count(view, "\n")+1 > size.Height {
					t.Fatalf("login exceeds height %d: %s", size.Height, view)
				}
			}
			if test.action == "esc" || test.action == "ctrl+c" {
				key := tea.KeyPressMsg{Code: tea.KeyEsc}
				if test.action == "ctrl+c" {
					key = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
				}
				next, cmd := m.Update(key)
				m = next.(model)
				if cmd != nil {
					if _, quits := cmd().(tea.QuitMsg); quits {
						t.Fatal("canceling private input exited TUI")
					}
				}
				select {
				case <-called:
					t.Fatal("canceled key submitted")
				default:
				}
			} else {
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
				if !m.busy || m.keyInput != nil {
					t.Fatal("key retained while submitting")
				}
				if key := <-called; key != secret {
					t.Fatal("login did not receive entered key")
				}
				for _, key := range []tea.Msg{
					tea.KeyPressMsg{Text: "ignored during login"},
					tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift},
					tea.PasteMsg{Content: secret},
				} {
					next, _ = m.Update(key)
					m = next.(model)
				}
				if m.input.Value() != "" {
					t.Fatal("login work enabled the normal composer")
				}
				if test.action == "cancel" {
					next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
					m = next.(model)
				}
				done := <-m.events
				next, _ = m.Update(done)
				m = next.(model)
			}
			if m.busy || m.loggingIn || m.keyInput != nil || m.loginText != "" || strings.Contains(m.View().Content, secret) || m.input.Value() != "" {
				t.Fatal("login state retained")
			}
			if len(a.Messages()) != 0 || len(store.Path()) != 1 {
				t.Fatal("login entered durable conversation")
			}
			if !m.loginRequired {
				t.Fatal("Go login unlocked unrelated Codex profile")
			}
			if test.action == "save" {
				if len(m.profiles) != 2 || !strings.Contains(m.notice, "key saved") {
					t.Fatal("imported profiles not available")
				}
				m.configureModel(modelCommand, "opencode-go/fixture")
				if m.loginRequired || m.profile != goProfile {
					t.Fatal("imported model could not be selected")
				}
			} else if test.action == "fail" {
				if !strings.Contains(m.notice, "catalog unavailable") || len(m.profiles) != 1 {
					t.Fatal("failed login changed profiles")
				}
			} else if m.notice != "Login canceled" {
				t.Fatalf("notice %q", m.notice)
			}
		})
	}
}
