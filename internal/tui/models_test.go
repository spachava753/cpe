package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

const lowEffort = "low"

func TestModelAndReasoningCommands(t *testing.T) {
	const firstProfile, secondProfile, thirdProfile = "alpha", "beta", "gamma"
	const highEffort = "high"
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "models.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profiles := map[string]config.Model{
		firstProfile:  {Provider: "codex", ID: "alpha-model", ReasoningEffort: "low"},
		secondProfile: {Provider: "responses", ID: "beta-model"},
		thirdProfile:  {Provider: "anthropic", ID: "gamma-model"},
		"gemini":      {Provider: "gemini", ID: "custom-gemini"},
		"compatible":  {Provider: "openai", ID: "custom-chat-model"},
		"missing-key": {Provider: "openai", ID: "unavailable"},
	}
	gen := agenttest.NewScriptedGenerator()
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024}}, Model: profiles[firstProfile], Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), a, firstProfile)
	m.profiles = profiles
	m.needsLogin = func(profile config.Model) bool { return profile.Provider == "codex" }
	m.loginRequired = true
	m.newGenerator = func(_ context.Context, profile config.Model) (gai.Generator, error) {
		if profile.ID == "unavailable" {
			return nil, errors.New("missing API key")
		}
		return gen, nil
	}
	for _, step := range []struct {
		command, name, effort, notice string
		signedOut                     bool
	}{
		{"/reasoning high", firstProfile, highEffort, "Reasoning: high", true},
		{"/model alpha", firstProfile, highEffort, "Model: alpha", true},
		{"/reasoning invalid", firstProfile, highEffort, "Error:", true},
		{"/reasoning default", firstProfile, lowEffort, "Reasoning: low", true},
		{"/model missing-key", firstProfile, lowEffort, "missing API key", true},
		{"/model unknown", firstProfile, lowEffort, "unknown model profile", true},
		{"/model beta extra", firstProfile, lowEffort, "unknown model profile", true},
		{"/model\tbeta", secondProfile, "", "Model: beta", false},
		{"/reasoning none", secondProfile, "none", "Reasoning: none", false},
		{"/reasoning default", secondProfile, "", "Reasoning: provider default", false},
		{"/model gamma", thirdProfile, "", "Model: gamma", false},
		{"/reasoning high", thirdProfile, highEffort, "Reasoning: high", false},
		{"/reasoning adaptive", thirdProfile, "adaptive", "Reasoning: adaptive", false},
		{"/reasoning disabled", thirdProfile, "disabled", "Reasoning: disabled", false},
		{loginCommand, thirdProfile, "disabled", "Login is unavailable", false},
		{"/model gemini", "gemini", "", "Model: gemini", false},
		{"/reasoning high", "gemini", highEffort, "Reasoning: high", false},
		{"/reasoning default", "gemini", "", "Reasoning: provider default", false},
		{"/model compatible", "compatible", "", "Model: compatible", false},
		{"/reasoning high", "compatible", highEffort, "Reasoning: high", false},
		{"/model alpha", firstProfile, lowEffort, "sign in with /login", true},
	} {
		m.input.SetValue(step.command)
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(model)
		if m.name != step.name || m.profile.ReasoningEffort != step.effort || a.Model() != m.profile || m.loginRequired != step.signedOut || !strings.Contains(m.notice, step.notice) || m.busy || m.picker != nil {
			t.Fatalf("%q: model=%s effort=%s login=%t notice=%s", step.command, m.name, m.profile.ReasoningEffort, m.loginRequired, m.notice)
		}
	}
	if profiles[firstProfile].ReasoningEffort != lowEffort || len(a.Messages()) != 0 || len(store.Path()) != 1 {
		t.Fatal("commands changed defaults or entered conversation history")
	}
	// Bare commands are keyboard pickers; cancellation leaves all settings alone.
	m.input.SetValue(modelCommand)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.picker == nil || m.picker.list.SelectedItem().(choice).value != firstProfile {
		t.Fatal("picker did not start at current profile")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.name != secondProfile || m.loginRequired || m.picker != nil {
		t.Fatal("picker did not apply model selection")
	}
	m.input.SetValue(reasoningCommand)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}, {Width: 24, Height: 8}} {
		next, _ = m.Update(size)
		m = next.(model)
		for line := range strings.SplitSeq(m.View(), "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("picker line exceeds %d: %q", size.Width, line)
			}
		}
		if strings.Count(m.View(), "\n")+1 > size.Height {
			t.Fatalf("picker exceeds height %d:\n%s", size.Height, m.View())
		}
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.profile.ReasoningEffort != "" || m.picker != nil {
		t.Fatal("cancel changed effort")
	}
	m.input.SetValue(reasoningCommand)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.profile.ReasoningEffort != "none" || m.picker != nil {
		t.Fatal("picker did not apply reasoning selection")
	}
	for _, name := range []string{firstProfile, secondProfile, thirdProfile, "gemini", "compatible"} {
		t.Run("reasoning picker for "+name, func(t *testing.T) {
			m.configureModel(modelCommand, name)
			m.configureModel(reasoningCommand, "")
			if m.picker == nil || m.picker.command != reasoningCommand {
				t.Fatal("missing reasoning picker")
			}
			for i, item := range m.picker.list.Items() {
				if item.(choice).value == highEffort {
					m.picker.list.Select(i)
					break
				}
			}
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(model)
			if m.profile.ReasoningEffort != highEffort || m.picker != nil || !strings.Contains(ansi.Strip(m.View()), highEffort) {
				t.Fatalf("effort not selected/displayed: %s", m.View())
			}
		})
	}
	m.configureModel(modelCommand, secondProfile)
	// Busy turns must not permit settings mutations or picker activation.
	m.busy = true
	m.input.SetValue("/model alpha")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.name != secondProfile || m.picker != nil {
		t.Fatal("model changed during generation")
	}
}
