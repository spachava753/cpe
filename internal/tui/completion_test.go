package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	for _, test := range []struct {
		name                 string
		busy, picker, narrow bool
	}{
		{name: "idle completion help"}, {name: "busy completion help", busy: true},
		{name: "busy narrow completion help", busy: true, narrow: true},
		{name: "idle picker help", picker: true}, {name: "busy picker help", busy: true, picker: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := newModel(t.Context(), a, "fixture")
			m.profiles = map[string]config.Model{"fixture": profile}
			canceled := false
			m.busy = test.busy
			m.cancel = func() { canceled = true }
			if test.narrow {
				m.width = 59
			}
			m.input.SetValue("/mo")
			m.syncCompletion()
			key := tea.KeyPressMsg{Code: tea.KeyEsc}
			want := "Esc dismiss"
			if test.busy {
				want = "Esc cancel work"
			}
			if test.picker {
				m.configureModel(modelCommand, "")
				m.syncCompletion()
				key = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
				want = "Ctrl+C quit"
				if test.busy {
					want = "Ctrl+C cancel work"
				}
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q: %s", want, view)
			}
			next, cmd := m.Update(key)
			m = next.(model)
			if canceled != test.busy || m.input.Value() != "/mo" {
				t.Fatalf("canceled=%v draft=%q", canceled, m.input.Value())
			}
			if test.picker {
				if test.busy && m.picker != nil {
					t.Fatal("canceled work retained picker")
				}
				if !test.busy {
					if cmd == nil {
						t.Fatal("idle picker did not quit")
					}
					if _, ok := cmd().(tea.QuitMsg); !ok {
						t.Fatal("idle picker did not quit")
					}
				}
			} else if m.completion.dismissed == test.busy {
				t.Fatalf("completion dismissal=%v busy=%v", m.completion.dismissed, test.busy)
			}
		})
	}

	t.Run("filter and run a picker", func(t *testing.T) {
		m := newModel(t.Context(), a, "fixture")
		m.profiles = map[string]config.Model{"fixture": profile}
		next, _ := m.Update(tea.KeyPressMsg{Text: "/"})
		m = next.(model)
		if m.completionHeight() != 7 || !strings.Contains(ansi.Strip(m.completionView()), "Choose a model") {
			t.Fatal("slash did not open the five-row popup")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		m = next.(model)
		if !strings.Contains(ansi.Strip(m.completionView()), exitCommand) || m.input.Value() != "/" {
			t.Fatal("up did not wrap to the last command or changed the draft")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = next.(model)
		if m.completion.selected != 0 {
			t.Fatal("down did not wrap to the first command")
		}
		next, _ = m.Update(tea.KeyPressMsg{Text: "mo"})
		m = next.(model)
		if len(m.completion.matches) != 1 || m.completion.matches[0].name != modelCommand {
			t.Fatal("command prefix did not filter suggestions")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		if m.picker == nil || m.completionHeight() != 0 || m.input.Value() != "" || m.busy {
			t.Fatal("Enter did not open the completed model picker")
		}
	})

	for _, lock := range []struct {
		name string
		mod  tea.KeyMod
	}{
		{"caps lock", tea.ModCapsLock}, {"num lock", tea.ModNumLock},
		{"both locks", tea.ModCapsLock | tea.ModNumLock},
	} {
		for _, action := range []struct {
			name              string
			code              rune
			want              string
			selected          int
			picker, dismissed bool
		}{
			{name: "enter dispatches", code: tea.KeyEnter, picker: true},
			{name: "tab completes", code: tea.KeyTab, want: "/model ", dismissed: true},
			{name: "down selects", code: tea.KeyDown, want: "/", selected: 1},
			{name: "up wraps", code: tea.KeyUp, want: "/", selected: len(slashCommands) - 1},
			{name: "escape dismisses", code: tea.KeyEsc, want: "/", dismissed: true},
		} {
			t.Run(lock.name+"/"+action.name, func(t *testing.T) {
				m := newModel(t.Context(), a, "fixture")
				m.profiles = map[string]config.Model{"fixture": profile}
				m.input.SetValue("/")
				m.syncCompletion()
				next, _ := m.Update(tea.KeyPressMsg{Code: action.code, Mod: lock.mod})
				m = next.(model)
				if m.input.Value() != action.want || (m.picker != nil) != action.picker || m.completion.selected != action.selected || m.completion.dismissed != action.dismissed {
					t.Fatalf("lock state changed completion: input=%q picker=%v completion=%+v", m.input.Value(), m.picker, m.completion)
				}
			})
		}
	}

	t.Run("shift submit preference and picker isolation", func(t *testing.T) {
		m := newModel(t.Context(), a, "fixture")
		m.submitKey = config.SubmitShiftEnter
		m.profiles = map[string]config.Model{"fixture": profile}
		m.input.SetValue("/mo")
		m.syncCompletion()
		if !strings.Contains(m.completionHelp(), "Shift+Enter run") || !strings.Contains(ansi.Strip(m.View().Content), "Shift+Enter run") {
			t.Fatal("completion help did not follow the submit preference")
		}
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		if m.input.Value() != "/mo\n" || m.completionHeight() != 0 || m.picker != nil || m.busy {
			t.Fatal("Enter dispatched completion instead of inserting a newline")
		}
		m.input.SetValue("/mo")
		m.syncCompletion()
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift | tea.ModCapsLock})
		m = next.(model)
		if m.picker == nil || m.input.Value() != "" || m.busy {
			t.Fatal("Shift+Enter did not dispatch the completed command")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		m = next.(model)
		if m.picker == nil {
			t.Fatal("composer preference changed picker confirmation")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		if m.picker != nil || len(a.Messages()) != 0 || !strings.Contains(ansi.Strip(m.View().Content), "Shift+Enter send · Enter newline") {
			t.Fatal("picker confirmation, history isolation, or composer help changed")
		}
	})

	t.Run("tab completes without running", func(t *testing.T) {
		m := newModel(t.Context(), a, "tab-fixture")
		for _, key := range []tea.KeyPressMsg{{Text: "/l"}, {Code: tea.KeyDown}, {Code: tea.KeyTab}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != loginDeviceCommand+" " || m.completionHeight() != 0 || m.busy {
			t.Fatal("Tab did not fill the selected compound command without executing")
		}
	})

	t.Run("escape suppresses until slash is removed", func(t *testing.T) {
		m := newModel(t.Context(), a, "dismiss-fixture")
		for _, key := range []tea.KeyPressMsg{{Text: "/t"}, {Code: tea.KeyEsc}, {Text: "h"}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != "/th" || m.completionHeight() != 0 || !m.completion.literal {
			t.Fatal("dismissal lost the draft or reopened while typing")
		}
		for range 3 {
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			m = next.(model)
		}
		next, _ := m.Update(tea.KeyPressMsg{Text: "/"})
		m = next.(model)
		if m.completionHeight() == 0 || m.completion.literal {
			t.Fatal("a new slash did not reactivate suggestions")
		}
	})

	t.Run("required argument stays editable", func(t *testing.T) {
		m := newModel(t.Context(), a, "branch-fixture")
		for _, key := range []tea.KeyPressMsg{{Text: "/br"}, {Code: tea.KeyEnter}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != branchCommand+" " || m.completionHeight() != 0 || m.busy {
			t.Fatal("branch executed without a checkpoint ID")
		}
	})

	t.Run("dismissal preserves recognized command behavior", func(t *testing.T) {
		m := newModel(t.Context(), a, "help-fixture")
		for _, key := range []tea.KeyPressMsg{{Text: helpCommand}, {Code: tea.KeyEsc}, {Code: tea.KeyEnter}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if !strings.Contains(m.notice, modelCommand) || m.busy || m.completion.dismissed {
			t.Fatal("dismissal turned a recognized command into a prompt")
		}
	})

	t.Run("unknown commands still error without dismissal", func(t *testing.T) {
		m := newModel(t.Context(), a, "typo-fixture")
		for _, key := range []tea.KeyPressMsg{{Text: "/typo"}, {Code: tea.KeyEnter}} {
			next, _ := m.Update(key)
			m = next.(model)
		}
		if !strings.Contains(m.notice, "Unknown command") || m.busy {
			t.Fatal("command typo unexpectedly sent to the agent")
		}
	})

	t.Run("invisible suggestions do not consume Enter", func(t *testing.T) {
		m := newModel(t.Context(), a, "tiny-fixture")
		for _, msg := range []tea.Msg{tea.WindowSizeMsg{Width: 24, Height: 8}, tea.KeyPressMsg{Text: "/mo"}, tea.KeyPressMsg{Code: tea.KeyEnter}} {
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
			next, _ := m.Update(tea.PasteMsg{Content: text})
			m = next.(model)
			if m.completionHeight() != 0 || m.input.Value() != text {
				t.Fatal("ordinary text, arguments or multiline paste opened completion")
			}
		})
	}

	t.Run("cursor movement and multiline editing", func(t *testing.T) {
		m := newModel(t.Context(), a, "cursor-fixture")
		next, _ := m.Update(tea.KeyPressMsg{Text: "/rea"})
		m = next.(model)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		m = next.(model)
		if m.completionHeight() != 0 {
			t.Fatal("completion stayed open with cursor inside a word")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		m = next.(model)
		if m.completionHeight() == 0 {
			t.Fatal("completion did not return at end of input")
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		m = next.(model)
		if m.input.Value() != "/rea\n" || m.completionHeight() != 0 {
			t.Fatal("Shift+Enter did not close completion and insert a newline")
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
	next, _ := m.Update(tea.KeyPressMsg{Text: "/"})
	m = next.(model)
	if m.viewport.YOffset() != 6 {
		t.Fatal("opening popup moved the conversation")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(model)
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
		if lipgloss.Width(m.View().Content) > size.Width || lipgloss.Height(m.View().Content) > size.Height {
			t.Fatalf("popup does not fit %+v", size)
		}
		text := ansi.Strip(m.View().Content)
		if m.completionHeight() > 0 && strings.Index(text, reasoningCommand) > strings.LastIndex(text, "› /") {
			t.Fatal("popup is not above the composer")
		}
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(model)
	if m.viewport.YOffset() != 6 || m.input.Value() != "/" {
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
	for _, key := range []tea.KeyPressMsg{{Text: "/"}, {Code: tea.KeyEsc}, {Text: "tmp is the directory to inspect"}, {Code: tea.KeyEnter}} {
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
