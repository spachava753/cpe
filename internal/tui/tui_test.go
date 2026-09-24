package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

type outputBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func TestModelRefresh(t *testing.T) {
	for _, test := range []struct {
		name         string
		block        gai.Block
		want, absent string
	}{
		{"text", gai.TextBlock("printed output"), "printed output", "[Image:"},
		{"image", gai.ImageBlock([]byte("fixture bytes"), "image/png"), "[Image: image/png]", "Zml4dHVyZSBieXRlcw=="},
		{"thinking", gai.Block{BlockType: gai.Thinking, Content: gai.Str("private reasoning")}, "Starlark", "private reasoning"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := model{viewport: viewport.New(80, 20), messages: gai.Dialog{gai.ToolResultMessage("call", test.block)}}
			m.refresh(true)
			view := ansi.Strip(m.viewport.View())
			if !strings.Contains(view, test.want) || strings.Contains(view, test.absent) {
				t.Fatalf("transcript=%q", view)
			}
		})
	}
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *outputBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.String() }

func TestProgramKeyboardRenderAndDurableTurn(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	call, err := gai.ToolCallBlock("ui-call", "starlark_repl", map[string]any{"code": "answer = 6 * 7; print(answer)"})
	if err != nil {
		t.Fatal(err)
	}
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("The answer is 42.")}}}, FinishReason: gai.EndTurn}},
	)
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{System: "Test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}}, Model: config.Model{ID: "test"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	output := &outputBuffer{}
	program := tea.NewProgram(newModel(t.Context(), a, "test"), tea.WithInput(input), tea.WithOutput(output), tea.WithoutSignalHandler(), tea.WithoutCatchPanics())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	t.Cleanup(func() { program.Kill() })
	program.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("compute")})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !strings.Contains(output.text(), "Saved") {
		select {
		case <-deadline:
			t.Fatalf("TUI never finished:\n%s", ansi.Strip(output.text()))
		case <-tick.C:
		}
	}
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(quitCommand)})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TUI did not exit")
	}
	rendered := ansi.Strip(output.text())
	if !strings.Contains(rendered, "The answer is 42.") || !strings.Contains(rendered, "starlark_repl") {
		t.Fatalf("missing output:\n%s", rendered)
	}
	if len(a.Messages()) != 4 {
		t.Fatal("turn not durable")
	}
}

type waitingGenerator struct{ started chan struct{} }

func (g waitingGenerator) Generate(ctx context.Context, _ gai.GenerationRequest) (gai.Response, error) {
	close(g.started)
	<-ctx.Done()
	return gai.Response{}, ctx.Err()
}

func TestCancellationKeepsUIUsableAndResizeFits(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := make(chan struct{})
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{System: "Test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}}, Model: config.Model{ID: "test"}, Generator: waitingGenerator{started}, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(t.Context(), a, "test")
	m.input.SetValue("wait")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	<-started
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if !m.busy {
		t.Fatal("cancellation did not wait for worker")
	}
	for {
		u := <-m.events
		next, _ = m.Update(u)
		m = next.(model)
		if u.done {
			break
		}
	}
	if m.busy || !strings.Contains(m.notice, "Canceled") {
		t.Fatal(m.notice)
	}
	m.notice = "Error: first line\nsecond line"
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}, {Width: 24, Height: 8}} {
		next, _ = m.Update(size)
		m = next.(model)
		view := m.View()
		for line := range strings.SplitSeq(view, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("line exceeds width %d: %q", size.Width, line)
			}
		}
		if strings.Count(view, "\n")+1 > size.Height {
			t.Fatalf("view exceeds height %d:\n%s", size.Height, view)
		}
	}
	m.input.SetValue("one")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = next.(model)
	if m.input.Value() != "one\n" {
		t.Fatalf("multiline input %q", m.input.Value())
	}
}

func TestTerminalControlSequencesAreRemoved(t *testing.T) {
	input := "hello\x1b]52;c;ZXZpbA==\a\x1b[2Jworld\x00\r\nnext"
	if got := clean(input); got != "helloworld\nnext" {
		t.Fatalf("unsafe render %q", got)
	}
}

func TestLoginIsCancellableAndNeverEntersConversation(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "login.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Ready after login.")}}}, FinishReason: gai.EndTurn}})
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{System: "Login test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}}, Model: config.Model{Provider: "codex", ID: "login-fixture"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ready := make(chan struct{})
	m := newModel(t.Context(), a, "test")
	m.loginRequired = true
	m.login = func(ctx context.Context, method string, notify func(string)) error {
		if method != "device" {
			return fmt.Errorf("unexpected login method")
		}
		notify("Open https://example.com/device\nEnter code: PRIVATE-CODE\n" + strings.Repeat("Long login instructions ", 20))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
			return nil
		}
	}
	for attempt := range 2 {
		m.input.SetValue("/login device")
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(model)
		progress := <-m.events
		next, _ = m.Update(progress)
		m = next.(model)
		if !strings.Contains(m.viewport.View(), "PRIVATE-CODE") {
			t.Fatal("missing login instructions")
		}
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}} {
			next, _ = m.Update(size)
			m = next.(model)
			for line := range strings.SplitSeq(m.View(), "\n") {
				if ansi.StringWidth(line) > size.Width {
					t.Fatal("login view exceeds terminal width")
				}
			}
			if strings.Count(m.View(), "\n")+1 > size.Height {
				t.Fatal("login view exceeds terminal height")
			}
		}
		if attempt == 0 {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = next.(model)
		} else {
			close(ready)
		}
		completed := <-m.events
		next, _ = m.Update(completed)
		m = next.(model)
		if m.busy || m.loggingIn || m.loginText != "" || strings.Contains(m.View(), "PRIVATE-CODE") {
			t.Fatal("login state leaked after completion")
		}
		if len(a.Messages()) != 0 || len(store.Path()) != 1 {
			t.Fatal("login was written to model history")
		}
		if attempt == 0 && (!m.loginRequired || m.notice != "Login canceled") {
			t.Fatal("cancellation did not preserve signed-out state")
		}
	}
	if m.loginRequired || !strings.Contains(m.notice, "Signed in") {
		t.Fatal("successful login did not unlock prompting")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	m.input.SetValue("hello")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	for {
		u := <-m.events
		next, _ = m.Update(u)
		m = next.(model)
		if u.done {
			break
		}
	}
	if len(a.Messages()) != 2 || !strings.Contains(m.viewport.View(), "Ready after login.") {
		t.Fatal("conversation unusable after login")
	}
}
