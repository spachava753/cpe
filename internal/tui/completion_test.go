package tui

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/theme"
)

func TestSlashCompletionEditingAndNavigation(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "completion.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := config.Model{Provider: "codex", ID: "completion-model"}
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}}, Model: profile, Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	t.Run("filter and run a picker", func(t *testing.T) {
		m := newModel(t.Context(), a, "fixture")
		m.profiles = map[string]config.Model{"fixture": profile}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		m = next.(model)
		if m.completionHeight() != 7 || !strings.Contains(ansi.Strip(m.completionView()), "Choose a model") {
			t.Fatal("slash did not open the five-row popup")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(model)
		if !strings.Contains(ansi.Strip(m.completionView()), exitCommand) || m.input.Value() != "/" {
			t.Fatal("up did not wrap to the last command or changed the draft")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
		if m.completion.selected != 0 {
			t.Fatal("down did not wrap to the first command")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mo")})
		m = next.(model)
		if len(m.completion.matches) != 1 || m.completion.matches[0].name != modelCommand {
			t.Fatal("command prefix did not filter suggestions")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(model)
		if m.picker == nil || m.completionHeight() != 0 || m.input.Value() != "" || m.busy {
			t.Fatal("Enter did not open the completed model picker")
		}
	})

	t.Run("tab completes without running", func(t *testing.T) {
		m := newModel(t.Context(), a, "tab-fixture")
		for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/l")}, {Type: tea.KeyDown}, {Type: tea.KeyTab}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != loginDeviceCommand+" " || m.completionHeight() != 0 || m.busy {
			t.Fatal("Tab did not fill the selected compound command without executing")
		}
	})

	t.Run("escape suppresses until slash is removed", func(t *testing.T) {
		m := newModel(t.Context(), a, "dismiss-fixture")
		for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/t")}, {Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("h")}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != "/th" || m.completionHeight() != 0 || !m.completion.literal {
			t.Fatal("dismissal lost the draft or reopened while typing")
		}
		for range 3 {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
			m = next.(model)
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		m = next.(model)
		if m.completionHeight() == 0 || m.completion.literal {
			t.Fatal("a new slash did not reactivate suggestions")
		}
	})

	t.Run("required argument stays editable", func(t *testing.T) {
		m := newModel(t.Context(), a, "branch-fixture")
		for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/br")}, {Type: tea.KeyEnter}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != branchCommand+" " || m.completionHeight() != 0 || m.busy {
			t.Fatal("branch executed without a checkpoint ID")
		}
	})

	t.Run("dismissal preserves recognized command behavior", func(t *testing.T) {
		m := newModel(t.Context(), a, "help-fixture")
		for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune(helpCommand)}, {Type: tea.KeyEsc}, {Type: tea.KeyEnter}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if !strings.Contains(m.notice, modelCommand) || m.busy || m.completion.dismissed {
			t.Fatal("dismissal turned a recognized command into a prompt")
		}
	})

	t.Run("unknown commands still error without dismissal", func(t *testing.T) {
		m := newModel(t.Context(), a, "typo-fixture")
		for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/typo")}, {Type: tea.KeyEnter}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if !strings.Contains(m.notice, "Unknown command") || m.busy {
			t.Fatal("command typo unexpectedly sent to the agent")
		}
	})

	t.Run("invisible suggestions do not consume Enter", func(t *testing.T) {
		m := newModel(t.Context(), a, "tiny-fixture")
		for _, msg := range []tea.Msg{tea.WindowSizeMsg{Width: 24, Height: 8}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/mo")}, tea.KeyMsg{Type: tea.KeyEnter}} {
			next, _ := m.Update(msg)
			m = next.(model)
		}
		if m.picker != nil || !strings.Contains(m.notice, "Unknown command") {
			t.Fatal("hidden suggestion captured Enter")
		}
	})

	for _, text := range []string{"read /tmp", "/unknown", "/model named-profile", " /help", "/help\nsecond line", "https://example.com/"} {
		t.Run(text, func(t *testing.T) {
			m := newModel(t.Context(), a, "text-fixture")
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
			m = next.(model)
			if m.completionHeight() != 0 || m.input.Value() != text {
				t.Fatal("ordinary text, arguments or multiline paste opened completion")
			}
		})
	}

	t.Run("cursor movement and multiline editing", func(t *testing.T) {
		m := newModel(t.Context(), a, "cursor-fixture")
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/rea")})
		m = next.(model)
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
		m = next.(model)
		if m.completionHeight() != 0 {
			t.Fatal("completion stayed open with cursor inside a word")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(model)
		if m.completionHeight() == 0 {
			t.Fatal("completion did not return at end of input")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
		m = next.(model)
		if m.input.Value() != "/rea\n" || m.completionHeight() != 0 {
			t.Fatal("Alt+Enter did not close completion and insert a newline")
		}
	})
	if len(a.Messages()) != 0 || len(store.Path()) != 1 {
		t.Fatal("completion or local commands entered the session")
	}
}

func TestCompletionThemeResizeAndScroll(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "popup.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}}, Model: config.Model{ID: "popup-model"}, Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), a, "popup-fixture")
	m.messages = gai.Dialog{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(strings.Repeat("Conversation history\n", 70))}}}
	m.refresh(false)
	m.viewport.SetYOffset(6)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = next.(model)
	if m.viewport.YOffset != 6 {
		t.Fatal("opening popup moved the conversation")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	m.renderer = lipgloss.NewRenderer(io.Discard)
	m.renderer.SetColorProfile(termenv.TrueColor)
	palette := theme.Default()
	palette.Colors.SelectionBackground = "#334455"
	next, _ = m.Update(themeUpdate{theme: palette})
	m = next.(model)
	if m.completion.selected != 1 || m.input.Value() != "/" || !strings.Contains(m.completionView(), "48;2;51;68;85") {
		t.Fatal("theme reload lost completion or did not restyle the selection")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 28}, {Width: 40, Height: 14}, {Width: 24, Height: 8}, {Width: 80, Height: 24}} {
		next, _ = m.Update(size)
		m = next.(model)
		if lipgloss.Width(m.View()) > size.Width || lipgloss.Height(m.View()) > size.Height {
			t.Fatalf("popup does not fit %+v", size)
		}
		text := ansi.Strip(m.View())
		if m.completionHeight() > 0 && strings.Index(text, reasoningCommand) > strings.LastIndex(text, "› /") {
			t.Fatal("popup is not above the composer")
		}
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.viewport.YOffset != 6 || m.input.Value() != "/" {
		t.Fatal("dismissing popup changed scroll or input")
	}
}

func TestDismissedSlashCanBeSentAsLiteralText(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "literal.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Understood.")}}}, FinishReason: gai.EndTurn}})
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}}, Model: config.Model{ID: "literal-model"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), a, "literal-fixture")
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/")}, {Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("tmp is the directory to inspect")}, {Type: tea.KeyEnter}} {
		next, _ := m.Update(key)
		m = next.(model)
	}
	if !m.busy {
		t.Fatalf("literal slash rejected: %s", m.notice)
	}
	defer m.cancel()
	deadline := time.After(5 * time.Second)
	for m.busy {
		select {
		case event := <-m.events:
			next, _ := m.Update(event)
			m = next.(model)
		case <-deadline:
			t.Fatal("literal prompt did not finish")
		}
	}
	if len(a.Messages()) != 2 || a.Messages()[0].Blocks[0].Content.String() != "/tmp is the directory to inspect" || m.completion.dismissed {
		t.Fatalf("literal prompt changed or suppression survived sending: messages=%+v completion=%+v notice=%s", a.Messages(), m.completion, m.notice)
	}
}
