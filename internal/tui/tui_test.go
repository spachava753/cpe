package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
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
			m := model{viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)), messages: gai.Dialog{gai.ToolResultMessage("call", test.block)}}
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
	for _, tc := range []struct{ name, input, want string }{
		{"enter submits", "compute\r", "compute"},
		{"shift enter newline", "compute\x1b[13;2umore\r", "compute\nmore"},
		{"control J newline", "compute\nmore\r", "compute\nmore"},
		{"bracketed multiline paste", "\x1b[200~compute\nmore\x1b[201~\r", "compute\nmore"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			program := tea.NewProgram(newModel(t.Context(), a, "test"), tea.WithInput(input), tea.WithOutput(output), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler(), tea.WithoutCatchPanics())
			done := make(chan error, 1)
			go func() { _, err := program.Run(); done <- err }()
			t.Cleanup(func() { program.Kill() })
			if _, err := io.WriteString(writer, tc.input); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(5 * time.Second)
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for !strings.Contains(output.text(), "Saved") {
				select {
				case err := <-done:
					t.Fatalf("TUI exited before finishing: %v", err)
				case <-deadline:
					t.Fatalf("TUI never finished:\n%s", ansi.Strip(output.text()))
				case <-tick.C:
				}
			}
			program.Send(tea.KeyPressMsg{Text: quitCommand})
			program.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
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
			if messages := a.Messages(); len(messages) != 4 || messages[0].Blocks[0].Content.String() != tc.want {
				t.Fatalf("durable conversation = %+v, want prompt %q", messages, tc.want)
			}
		})
	}
}

type waitingGenerator struct {
	started chan struct{}
	finish  chan error
	calls   atomic.Int32
}

func (g *waitingGenerator) Generate(ctx context.Context, _ gai.GenerationRequest) (gai.Response, error) {
	if g.calls.Add(1) == 1 {
		close(g.started)
		select {
		case err := <-g.finish:
			if err != nil {
				return gai.Response{}, err
			}
		case <-ctx.Done():
			return gai.Response{}, ctx.Err()
		}
	}
	return gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Ready")}}}, FinishReason: gai.EndTurn}, nil
}

func TestDraftDuringWork(t *testing.T) {
	for _, tc := range []struct {
		name, notice string
		err          error
		cancelKey    tea.KeyPressMsg
		slash        bool
	}{
		{name: "completion", notice: "Saved"},
		{name: "failure", err: errors.New("provider failed"), notice: "provider failed"},
		{name: "escape cancellation", cancelKey: tea.KeyPressMsg{Code: tea.KeyEsc}, notice: "Canceled"},
		{name: "control C cancellation", cancelKey: tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, notice: "Canceled"},
		{name: "slash draft after completion", notice: "Saved", slash: true},
		{name: "slash draft after cancellation", cancelKey: tea.KeyPressMsg{Code: tea.KeyEsc}, notice: "Canceled", slash: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			gen := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{System: "Test", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}}, Model: config.Model{ID: "test"}, Generator: gen, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "test")
			m.input.SetValue("wait")
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			defer func() {
				if m.busy {
					m.cancel()
					for u := range m.events {
						if u.done {
							break
						}
					}
				}
			}()
			select {
			case <-gen.started:
			case <-time.After(5 * time.Second):
				t.Fatal("generation did not start")
			}
			keys := []tea.Msg{
				tea.KeyPressMsg{Text: "nexx"}, tea.KeyPressMsg{Code: tea.KeyBackspace},
				tea.KeyPressMsg{Text: "t"}, tea.KeyPressMsg{Code: tea.KeyLeft},
				tea.KeyPressMsg{Text: "X"}, tea.KeyPressMsg{Code: tea.KeyBackspace}, tea.KeyPressMsg{Code: tea.KeyRight},
				tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, tea.PasteMsg{Content: "pasted\nlast"},
				tea.KeyPressMsg{Code: tea.KeyHome}, tea.KeyPressMsg{Text: "Edited "}, tea.KeyPressMsg{Code: tea.KeyLeft},
			}
			want := "next\npasted\nEdited last"
			if tc.slash {
				keys = []tea.Msg{tea.KeyPressMsg{Text: "/mo"}}
				want = "/mo"
			}
			for _, key := range keys {
				next, _ = m.Update(key)
				m = next.(model)
			}
			if m.input.Value() != want || m.completionHeight() != 0 || !strings.Contains(ansi.Strip(m.View().Content), "Draft next message") {
				t.Fatalf("busy draft = %q, popup height = %d", m.input.Value(), m.completionHeight())
			}
			row, column := m.input.Line(), m.input.LineInfo().ColumnOffset
			for _, msg := range []tea.Msg{
				update{event: agent.Event{Kind: agent.EventDelta, Text: "Still working"}},
				tea.KeyPressMsg{Code: tea.KeyEnter},
				tea.WindowSizeMsg{Width: 32, Height: 12},
				tea.WindowSizeMsg{Width: 80, Height: 24},
			} {
				next, _ = m.Update(msg)
				m = next.(model)
			}
			if m.input.Value() != want || m.input.Line() != row || m.input.LineInfo().ColumnOffset != column || gen.calls.Load() != 1 || !m.busy || m.picker != nil {
				t.Fatal("streaming, Enter, or resizing changed the draft or submitted work")
			}
			if tc.cancelKey.Code != 0 {
				next, _ = m.Update(tc.cancelKey)
				m = next.(model)
				if !m.busy || m.input.Value() != want {
					t.Fatal("cancellation cleared the draft or skipped worker cleanup")
				}
			} else {
				gen.finish <- tc.err
			}
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case u := <-m.events:
					next, _ = m.Update(u)
					m = next.(model)
				case <-deadline:
					t.Fatal("worker did not finish")
				}
			}
			if !strings.Contains(m.notice, tc.notice) || m.input.Value() != want || m.input.Line() != row || m.input.LineInfo().ColumnOffset != column || gen.calls.Load() != 1 {
				t.Fatalf("completion lost the draft or queued it: draft=%q, notice=%q, calls=%d", m.input.Value(), m.notice, gen.calls.Load())
			}
			for _, msg := range a.Messages() {
				if msg.Role == gai.User && msg.Blocks[0].Content.String() != "wait" {
					t.Fatal("unsent draft entered the conversation")
				}
			}
			if tc.slash {
				if len(m.completion.matches) != 1 || m.completion.matches[0].name != modelCommand || m.completionHeight() == 0 {
					t.Fatal("slash completion did not resume")
				}
				return
			}
			// The same draft can be sent explicitly after the worker has stopped.
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(model)
			for m.busy {
				select {
				case u := <-m.events:
					next, _ = m.Update(u)
					m = next.(model)
				case <-deadline:
					t.Fatal("draft submission did not finish")
				}
			}
			messages := a.Messages()
			if gen.calls.Load() != 2 || messages[len(messages)-2].Blocks[0].Content.String() != want || m.input.Value() != "" {
				t.Fatal("idle Enter did not submit the preserved draft once")
			}
			m.notice = "Error: first line\nsecond line"
			for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}, {Width: 24, Height: 8}} {
				next, _ = m.Update(size)
				m = next.(model)
				view := m.View().Content
				for line := range strings.SplitSeq(view, "\n") {
					if ansi.StringWidth(line) > size.Width {
						t.Fatalf("line exceeds width %d: %q", size.Width, line)
					}
				}
				if strings.Count(view, "\n")+1 > size.Height {
					t.Fatalf("view exceeds height %d:\n%s", size.Height, view)
				}
			}
		})
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
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(model)
		progress := <-m.events
		next, _ = m.Update(progress)
		m = next.(model)
		if !strings.Contains(m.viewport.View(), "PRIVATE-CODE") {
			t.Fatal("missing login instructions")
		}
		for _, key := range []tea.Msg{tea.PasteMsg{Content: "ignored during login"}, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}} {
			next, _ = m.Update(key)
			m = next.(model)
		}
		if m.input.Value() != "" {
			t.Fatal("OAuth enabled the normal composer")
		}
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 32, Height: 12}} {
			next, _ = m.Update(size)
			m = next.(model)
			for line := range strings.SplitSeq(m.View().Content, "\n") {
				if ansi.StringWidth(line) > size.Width {
					t.Fatal("login view exceeds terminal width")
				}
			}
			if strings.Count(m.View().Content, "\n")+1 > size.Height {
				t.Fatal("login view exceeds terminal height")
			}
		}
		if attempt == 0 {
			next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			m = next.(model)
		} else {
			close(ready)
		}
		completed := <-m.events
		next, _ = m.Update(completed)
		m = next.(model)
		if m.busy || m.loggingIn || m.loginText != "" || strings.Contains(m.View().Content, "PRIVATE-CODE") {
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
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
