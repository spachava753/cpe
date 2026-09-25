package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/theme"
)

const lowEffort = "low"

func TestModelUpdatePicker(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "filter.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profiles := map[string]config.Model{
		"alpha":      {Provider: "codex", ID: "alpha-model"},
		"beta":       {Provider: "anthropic", ID: "claude-sonnet"},
		"gemini":     {Provider: "gemini", ID: "gemini-flash"},
		"team/模型":    {Provider: "openai", ID: "custom"},
		"jkhflguvd?": {Provider: "openai", ID: "shortcuts"},
	}
	for i := range 30 {
		profiles[fmt.Sprintf("catalog-%02d", i)] = config.Model{Provider: "responses", ID: fmt.Sprintf("model-%02d", i)}
	}
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024}}, Model: profiles["alpha"], Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, test := range []struct {
		name, query, wantQuery, selected string
		actions                          []tea.Msg
		matches                          int
	}{
		{name: "initial selection", selected: "alpha", matches: len(profiles)},
		{name: "fuzzy noncontiguous name", query: "bta", wantQuery: "bta", selected: "beta", matches: 1},
		{name: "case insensitive", query: "GMN", wantQuery: "GMN", selected: "gemini", matches: 1},
		{name: "provider", query: "anthropic", wantQuery: "anthropic", selected: "beta", matches: 1},
		{name: "model id", query: "snnt", wantQuery: "snnt", selected: "beta", matches: 1},
		{name: "unicode name", query: "模型", wantQuery: "模型", selected: "team/模型", matches: 1},
		{name: "slash in profile", query: "team/", wantQuery: "team/", selected: "team/模型", matches: 1},
		{name: "list shortcuts are text", query: "jkhflguvd?", wantQuery: "jkhflguvd?", selected: "jkhflguvd?", matches: 1},
		{name: "paste edits filter", actions: []tea.Msg{tea.PasteMsg{Content: "bta"}}, wantQuery: "bta", selected: "beta", matches: 1},
		{name: "backspace recovers matches", query: "betax", actions: []tea.Msg{tea.KeyPressMsg{Code: tea.KeyBackspace}}, wantQuery: "beta", selected: "beta", matches: 1},
		{name: "edit within query", query: "bta", actions: []tea.Msg{tea.KeyPressMsg{Code: tea.KeyHome}, tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Text: "e"}}, wantQuery: "beta", selected: "beta", matches: 1},
		{name: "clear from middle", query: "bta", actions: []tea.Msg{tea.KeyPressMsg{Code: tea.KeyLeft}, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}}, selected: "alpha", matches: len(profiles)},
		{name: "current marker not searchable", query: "current", wantQuery: "current"},
		{name: "no matches cannot apply or save", query: "zzzzz", actions: []tea.Msg{tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}}, wantQuery: "zzzzz"},
		{name: "navigate filtered matches", query: "catalog-", actions: []tea.Msg{tea.KeyPressMsg{Code: tea.KeyDown}}, wantQuery: "catalog-", selected: "catalog-01", matches: 30},
		{name: "lock modifiers", query: "catalog-", actions: []tea.Msg{tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCapsLock | tea.ModNumLock}}, wantQuery: "catalog-", selected: "catalog-01", matches: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := newModel(t.Context(), a, "alpha")
			m.profiles, m.configDir = profiles, dir
			m.input.SetValue("unsent draft")
			m.configureModel(modelCommand, "")
			for _, r := range test.query {
				next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				m = next.(model)
			}
			for _, msg := range test.actions {
				next, _ := m.Update(msg)
				m = next.(model)
			}
			if m.picker == nil || m.picker.list.FilterValue() != test.wantQuery || len(m.picker.list.VisibleItems()) != test.matches {
				t.Fatalf("unexpected filter or match count: %+v", m.picker)
			}
			item := m.picker.list.SelectedItem()
			if test.selected == "" {
				if item != nil || !strings.Contains(ansi.Strip(m.View().Content), "No matching models") {
					t.Fatal("empty matches retained a selection or omitted feedback")
				}
			} else if item == nil || item.(choice).value != test.selected {
				t.Fatalf("selected=%v, want %s", item, test.selected)
			}
			if test.name == "edit within query" && m.picker.list.FilterInput.Position() != 2 {
				t.Fatal("filter moved the editing cursor")
			}
			if m.input.Value() != "unsent draft" || m.savingDefault != nil || m.name != "alpha" || len(a.Messages()) != 0 || len(store.Path()) != 1 {
				t.Fatal("filter edited the draft, settings, or conversation")
			}
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			m = next.(model)
			if m.picker != nil || !m.input.Focused() || m.input.Value() != "unsent draft" {
				t.Fatal("dismissal did not restore the draft")
			}
			m.configureModel(modelCommand, "")
			if m.picker.list.FilterValue() != "" || m.picker.list.SelectedItem().(choice).value != "alpha" {
				t.Fatal("reopened picker retained the query")
			}
		})
	}
	t.Run("resize theme and filter cursor lifecycle", func(t *testing.T) {
		m := newModel(t.Context(), a, "alpha")
		m.profiles = profiles
		m.input.SetValue(modelCommand)
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		if cmd == nil || m.input.Focused() || !m.picker.list.FilterInput.Focused() {
			t.Fatal("picker did not take focus and start its cursor")
		}
		styles := m.picker.list.FilterInput.Styles()
		styles.Cursor.BlinkSpeed = time.Millisecond
		m.picker.list.FilterInput.SetStyles(styles)
		cmd = m.picker.list.FilterInput.Focus()
		for range 2 {
			before := m.picker.list.FilterInput.View()
			next, cmd = m.Update(cmd())
			m = next.(model)
			if m.picker.list.FilterInput.View() == before || cmd == nil {
				t.Fatal("filter cursor stopped blinking")
			}
		}
	})
	for _, test := range []struct{ name, query string }{
		{"paged matches", "catalog-"},
		{"long query middle cursor", strings.Repeat("a", 55) + "XYZ12"},
		{"wide query middle cursor", strings.Repeat("界", 60) + "XYZ12"},
	} {
		t.Run("resize and theme/"+test.name, func(t *testing.T) {
			m := newModel(t.Context(), a, "alpha")
			m.profiles = profiles
			m.configureModel(modelCommand, "")
			next, _ := m.Update(tea.PasteMsg{Content: test.query})
			m = next.(model)
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			m = next.(model)
			selected := m.picker.list.SelectedItem()
			if test.query == "catalog-" && selected.(choice).value == "catalog-00" {
				t.Fatal("paging did not move through matches")
			}
			for range 5 {
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
				m = next.(model)
			}
			position := m.picker.list.FilterInput.Position()
			themed := theme.Default()
			themed.Colors.Accent = "#123456"
			for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}, {Width: 24, Height: 8}, {Width: 100, Height: 30}} {
				next, _ = m.Update(size)
				m = next.(model)
				next, _ = m.Update(themeUpdate{theme: themed})
				m = next.(model)
				if m.picker.list.FilterValue() != test.query || m.picker.list.SelectedItem() != selected || m.picker.list.FilterInput.Position() != position {
					t.Fatal("layout/theme changed filter, selection, or cursor")
				}
				filter := m.picker.list.FilterInput.View()
				if !strings.Contains(filter, "38;2;18;52;86") {
					t.Fatal("filter missed theme colors")
				}
				if ansi.StringWidth(filter) > m.viewport.Width() {
					t.Fatalf("filter window exceeds available width %d: %q", m.viewport.Width(), ansi.Strip(filter))
				}
				if !strings.Contains(ansi.Strip(filter), string([]rune(test.query)[position])) {
					t.Fatal("filter window hides cursor character")
				}
				frame := m.View().Content
				for line := range strings.SplitSeq(frame, "\n") {
					if ansi.StringWidth(line) > size.Width {
						t.Fatalf("filter exceeds width %d", size.Width)
					}
				}
				if strings.Count(frame, "\n")+1 > size.Height {
					t.Fatalf("filter exceeds height %d", size.Height)
				}
			}
		})
	}
}

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
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if m.picker == nil || m.picker.list.SelectedItem().(choice).value != firstProfile {
		t.Fatal("picker did not start at current profile")
	}
	next, _ = m.Update(tea.PasteMsg{Content: "BTA"})
	m = next.(model)
	if m.input.Value() != "" || m.picker.list.FilterValue() != "BTA" || m.picker.list.SelectedItem().(choice).value != secondProfile || len(a.Messages()) != 0 {
		t.Fatal("pasted query failed to filter or entered the composer/conversation")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if m.name != secondProfile || m.loginRequired || m.picker != nil {
		t.Fatal("picker did not apply model selection")
	}
	m.input.SetValue(reasoningCommand)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}, {Width: 24, Height: 8}} {
		next, _ = m.Update(size)
		m = next.(model)
		for line := range strings.SplitSeq(m.View().Content, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("picker line exceeds %d: %q", size.Width, line)
			}
		}
		if strings.Count(m.View().Content, "\n")+1 > size.Height {
			t.Fatalf("picker exceeds height %d:\n%s", size.Height, m.View().Content)
		}
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(model)
	if m.profile.ReasoningEffort != "" || m.picker != nil {
		t.Fatal("cancel changed effort")
	}
	m.input.SetValue(reasoningCommand)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	m.picker.list.Select(0)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			if m.profile.ReasoningEffort != highEffort || m.picker != nil || !strings.Contains(ansi.Strip(m.View().Content), highEffort) {
				t.Fatalf("effort not selected/displayed: %s", m.View().Content)
			}
		})
	}
	t.Run("composer cursor lifecycle", func(t *testing.T) {
		for _, tc := range []struct {
			name, command string
			key           tea.KeyPressMsg
			busy          bool
		}{
			{"model dismiss", modelCommand, tea.KeyPressMsg{Code: tea.KeyEsc}, false},
			{"model apply", modelCommand, tea.KeyPressMsg{Code: tea.KeyEnter}, false},
			{"reasoning dismiss", reasoningCommand, tea.KeyPressMsg{Code: tea.KeyEsc}, false},
			{"reasoning apply", reasoningCommand, tea.KeyPressMsg{Code: tea.KeyEnter}, false},
			{"active picker cancellation", modelCommand, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := newModel(t.Context(), a, firstProfile)
				m.profiles, m.busy = profiles, tc.busy
				m.cancel = func() {}
				styles := m.input.Styles()
				styles.Cursor.BlinkSpeed = time.Millisecond
				m.input.SetStyles(styles)
				m.input.SetValue(tc.command)
				visible := m.input.View()
				// Enter the hidden phase before the picker takes focus.
				blink := m.input.Focus()
				next, _ := m.Update(blink())
				m = next.(model)
				if m.input.View() == visible {
					t.Fatal("cursor did not enter hidden phase")
				}
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
				if m.picker == nil || m.input.Focused() {
					t.Fatal("picker did not take composer focus")
				}
				next, cmd := m.Update(tc.key)
				m = next.(model)
				if m.picker != nil || !m.input.Focused() || cmd == nil {
					t.Fatal("picker did not restore focus with its cursor command")
				}
				for range 2 {
					before := m.input.View()
					messages := commandMessages(cmd)
					if len(messages) != 1 {
						t.Fatalf("expected one cursor timer, got %d messages", len(messages))
					}
					next, cmd = m.Update(messages[0])
					m = next.(model)
					if m.input.View() == before || cmd == nil {
						t.Fatal("restored cursor did not continue blinking")
					}
				}
			})
		}
	})
	m.configureModel(modelCommand, secondProfile)
	// Busy turns must not permit settings mutations or picker activation.
	m.busy = true
	m.input.SetValue("/model alpha")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if m.name != secondProfile || m.picker != nil {
		t.Fatal("model changed during generation")
	}
}
