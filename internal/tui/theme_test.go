package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/theme"
)

func TestThemeReloadPreservesUIAndRecoversFromErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "themed.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{System: "Theme test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}}, Model: config.Model{ID: "theme-fixture"}, Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), a, "theme-fixture")
	m.messages = gai.Dialog{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(strings.Repeat("A long conversation\n", 80))}}}
	m.refresh(false)
	m.viewport.SetYOffset(7)
	m.input.SetValue("unsent draft")
	m.input.CursorStart()
	m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.provisional = "Streaming reply"
	m.busy = true
	m.activity = "Working"
	themed := theme.Default()
	themed.Colors.Foreground = "#172838"
	themed.Colors.Background = "#f1f2f3"
	themed.Colors.Accent = "#123456"
	themed.Colors.Assistant = "#005599"
	themed.InputHeight = 5
	next, _ := m.Update(themeUpdate{theme: themed})
	m = next.(model)
	if m.input.Value() != "unsent draft" || m.input.LineInfo().ColumnOffset != 1 || m.viewport.YOffset() != 7 || !m.busy || m.provisional != "Streaming reply" || m.input.Height() != 5 {
		t.Fatalf("theme changed interaction state: draft=%q cursor=%+v scroll=%d busy=%t", m.input.Value(), m.input.LineInfo(), m.viewport.YOffset(), m.busy)
	}
	if !strings.Contains(m.input.View(), "38;2;18;52;86") || !strings.Contains(m.View().Content, "48;2;241;242;243") || !strings.Contains(m.viewport.View(), "38;2;23;40;56") {
		t.Fatalf("editor, background or conversation missed theme: %q", m.View().Content)
	}
	next, _ = m.Update(themeUpdate{err: errors.New("unfinished JSON edit")})
	m = next.(model)
	if m.theme != themed || !strings.Contains(ansi.Strip(m.View().Content), "Theme error:") || !strings.Contains(ansi.Strip(m.View().Content), "Esc to cancel") {
		t.Fatal("invalid reload lost theme, warning, or cancellation status")
	}
	m.busy = false
	m.profiles = map[string]config.Model{"a": {ID: "one"}, "b": {ID: "two"}}
	m.configureModel(modelCommand, "")
	m.picker.list.Select(1)
	themed.Colors.SelectionBackground = "#334455"
	next, _ = m.Update(themeUpdate{theme: themed})
	m = next.(model)
	if m.themeError != "" || m.picker == nil || m.picker.list.Index() != 1 || !strings.Contains(m.picker.list.View(), "48;2;51;68;85") {
		t.Fatal("picker selection or theme did not survive recovery")
	}
	m.picker = nil
	for _, command := range []string{treeCommand, usageCommand} {
		m.input.SetValue(command)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		before := ansi.Strip(m.viewport.View())
		themed.Bold = !themed.Bold
		next, _ = m.Update(themeUpdate{theme: themed})
		m = next.(model)
		if ansi.Strip(m.viewport.View()) != before {
			t.Fatalf("%s view lost on reload", command)
		}
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 28}, {Width: 32, Height: 12}, {Width: 24, Height: 8}} {
		next, _ = m.Update(size)
		m = next.(model)
		if lipgloss.Width(m.View().Content) > size.Width || lipgloss.Height(m.View().Content) > size.Height {
			t.Fatalf("themed UI does not fit %+v", size)
		}
	}
	if len(a.Messages()) != 0 {
		t.Fatal("presentation entered the conversation")
	}
}

func TestThemePickerAndDirectSelection(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "themes.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024}}, Model: config.Model{ID: "fixture"}, Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), a, "fixture")
	m.themeDir = dir
	m.loginRequired = true // Theme selection must work before login.
	m.messages = gai.Dialog{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(strings.Repeat("Conversation\n", 80))}}}
	m.refresh(false)
	m.viewport.SetYOffset(7)
	m.input.SetValue(themeCommand)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if m.picker == nil || len(m.picker.list.Items()) != 7 || m.picker.list.SelectedItem().(choice).value != "desktop" {
		t.Fatal("theme command must open a picker at the current theme")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(model)
	if m.picker != nil || m.theme != theme.Default() {
		t.Fatal("cancel changed the theme")
	}
	if _, err := os.Stat(filepath.Join(dir, "themes.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancel wrote configuration: %v", err)
	}
	m.input.SetValue(themeCommand)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	saved, err := theme.Load(dir, theme.Appearance{})
	if err != nil || m.picker != nil || m.theme.Name != "dracula" || m.theme != saved || m.viewport.YOffset() != 7 {
		t.Fatalf("picker failed to apply/save while preserving scroll: %+v %v", m.theme, err)
	}
	for _, command := range []string{"/theme nord", "/theme gruvbox", "/theme light", "/theme dark", "/theme desktop"} {
		m.input.SetValue(command)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		saved, err = theme.Load(dir, theme.Appearance{})
		if err != nil || m.theme != saved || m.theme.Name != strings.TrimPrefix(command, themeCommand+" ") || m.busy || !strings.Contains(m.notice, "Theme: ") {
			t.Fatalf("direct command %q: %+v %v", command, m.theme, err)
		}
	}
	// A result already in flight before selection must not revert the new theme.
	old := theme.Default()
	old.Name = "old selection"
	next, cmd := m.Update(themeUpdate{theme: old, revision: m.themeRevision - 1})
	m = next.(model)
	if m.theme != saved || cmd == nil {
		t.Fatal("stale poll reverted selection or stopped polling")
	}
	current := saved
	current.Colors.Accent = "5"
	next, _ = m.Update(themeUpdate{theme: current, revision: m.themeRevision})
	m = next.(model)
	if m.theme != current {
		t.Fatal("current poll must still apply external edits")
	}
	before, err := os.ReadFile(filepath.Join(dir, "themes.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("/theme unknown")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	after, err := os.ReadFile(filepath.Join(dir, "themes.json"))
	if err != nil || string(after) != string(before) || m.theme != current || !strings.Contains(m.notice, "unknown theme") {
		t.Fatalf("failed selection changed state: %s %v", m.notice, err)
	}
	if len(a.Messages()) != 0 || len(store.Path()) != 1 {
		t.Fatal("theme commands entered conversation history")
	}
}
