package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
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
	for _, tc := range []struct {
		name                     string
		blocks                   []gai.Block
		width                    int
		assistant, twice, failed bool
		needle                   string
		count                    int
		notice                   string
	}{
		{name: "below preview limit", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 19))}, width: 80, needle: "row", count: 19},
		{name: "exact preview limit", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 19) + "row")}, width: 80, needle: "row", count: 20},
		{name: "exact with trailing newline", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 20))}, width: 80, needle: "row", count: 20},
		{name: "one beyond preview", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 21))}, width: 80, needle: "row", count: 20, notice: "… 1 more line"},
		{name: "one budget across blocks", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 20)), gai.TextBlock("row\nrow")}, width: 80, needle: "row", count: 20, notice: "… 2 more lines"},
		{name: "wrapped single line", blocks: []gai.Block{gai.TextBlock(strings.Repeat("x", 41))}, width: 2, needle: "xx", count: 20, notice: "…"},
		{name: "wide characters", blocks: []gai.Block{gai.TextBlock(strings.Repeat("界", 64))}, width: 6, needle: "界", count: 60, notice: "… 2"},
		{name: "image within preview", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 19)), gai.ImageBlock([]byte("fixture"), "image/png")}, width: 80, needle: "[Image: image/png]", count: 1},
		{name: "image beyond preview", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 20)), gai.ImageBlock([]byte("fixture"), "image/png")}, width: 80, needle: "[Image: image/png]", count: 0, notice: "… 1 more line"},
		{name: "separate result budgets", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 21))}, twice: true, width: 80, needle: "row", count: 40, notice: "… 1 more line"},
		{name: "failed tool preview", blocks: []gai.Block{gai.TextBlock(strings.Repeat("error row\n", 30))}, failed: true, width: 80, needle: "error row", count: 20, notice: "… 10 more lines"},
		{name: "assistant stays complete", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\n", 30))}, assistant: true, width: 80, needle: "row", count: 30},
		{name: "terminal controls stripped", blocks: []gai.Block{gai.TextBlock(strings.Repeat("row\x1b[2J\n", 21))}, width: 80, needle: "row", count: 20, notice: "… 1 more line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := gai.ToolResultMessage("preview", tc.blocks...)
			message.ToolResultError = tc.failed
			if tc.assistant {
				message = gai.Message{Role: gai.Assistant, Blocks: tc.blocks}
			}
			m := model{input: textarea.New(), width: tc.width + 2, height: 110, viewport: viewport.New(viewport.WithWidth(tc.width), viewport.WithHeight(100)), messages: gai.Dialog{message}}
			if tc.twice {
				m.messages = append(m.messages, gai.ToolResultMessage("second", tc.blocks...))
			}
			before, err := json.Marshal(m.messages)
			if err != nil {
				t.Fatal(err)
			}
			themed := theme.Default()
			themed.Colors.Tool = "#123456"
			m.applyTheme(themed) // Include colored tool text and its reset sequences.
			m.viewport.SetWidth(tc.width)
			m.viewport.SetHeight(100)
			m.refresh(false)
			view := ansi.Strip(m.viewport.View())
			if count := strings.Count(view, tc.needle); count != tc.count {
				t.Fatalf("preview contains %d %q, want %d:\n%s", count, tc.needle, tc.count, view)
			}
			if tc.notice == "" && strings.Contains(view, "more lines") || tc.notice != "" && !strings.Contains(view, tc.notice) {
				t.Fatalf("truncation notice mismatch:\n%s", view)
			}
			after, err := json.Marshal(m.messages)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("display truncation changed the underlying messages")
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
	for _, tc := range []struct {
		name, input, want string
		submitKey         config.SubmitKey
	}{
		{"enter submits", "compute\r", "compute", config.SubmitEnter},
		{"shift enter newline", "compute\x1b[13;2umore\r", "compute\nmore", config.SubmitEnter},
		{"control J newline", "compute\nmore\r", "compute\nmore", config.SubmitEnter},
		{"shift submit and enter newline", "compute\rmore\x1b[13;2u", "compute\nmore", config.SubmitShiftEnter},
		{"shift submit and control J newline", "compute\nmore\x1b[13;2u", "compute\nmore", config.SubmitShiftEnter},
		{"shift submit with lock state", "compute\x1b[13;65umore\x1b[13;66u", "compute\nmore", config.SubmitShiftEnter},
		{"bracketed multiline paste", "\x1b[200~compute\nmore\x1b[201~\r", "compute\nmore", config.SubmitEnter},
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
			m := newModel(t.Context(), a, "test")
			m.submitKey = tc.submitKey
			program := tea.NewProgram(m, tea.WithInput(input), tea.WithOutput(output), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler(), tea.WithoutCatchPanics())
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
			submit := tea.KeyPressMsg{Code: tea.KeyEnter}
			if tc.submitKey == config.SubmitShiftEnter {
				submit.Mod = tea.ModShift
			}
			program.Send(submit)
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
		submitKey    config.SubmitKey
	}{
		{name: "completion", notice: "Saved"},
		{name: "shift submit completion", notice: "Saved", submitKey: config.SubmitShiftEnter},
		{name: "shift submit cancellation", notice: "Canceled", cancelKey: tea.KeyPressMsg{Code: tea.KeyEsc}, submitKey: config.SubmitShiftEnter},
		{name: "shift submit slash draft", notice: "Saved", slash: true, submitKey: config.SubmitShiftEnter},
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
			m.submitKey = tc.submitKey
			submit := tea.KeyPressMsg{Code: tea.KeyEnter}
			newline := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
			if tc.submitKey == config.SubmitShiftEnter {
				submit, newline = newline, submit
			}
			m.input.SetValue("wait")
			next, _ := m.Update(submit)
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
				newline, tea.PasteMsg{Content: "pasted\nlast"},
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
				submit,
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
			next, _ = m.Update(submit)
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

func TestModelUpdate(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "scroll.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var history strings.Builder
	for i := range 100 {
		fmt.Fprintf(&history, "History line %03d %s\n", i, strings.Repeat("word ", 10))
	}
	gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(history.String())}}}, FinishReason: gai.EndTurn}})
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 3}}, Model: config.Model{ID: "test"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "seed history", nil); err != nil {
		t.Fatal(err)
	}
	smallerInput := theme.Default()
	smallerInput.InputHeight = 1
	for _, tc := range []struct {
		name        string
		scroll      tea.Msg
		err         error
		layout      []tea.Msg
		inputHeight int
		reflow      bool
	}{
		{name: "page up through completion", scroll: tea.KeyPressMsg{Code: tea.KeyPgUp}},
		{name: "wheel through completion", scroll: tea.MouseWheelMsg{Button: tea.MouseWheelUp}},
		{name: "page up through failure", scroll: tea.KeyPressMsg{Code: tea.KeyPgUp}, err: errors.New("fixture failure")},
		{name: "page up through cancellation", scroll: tea.KeyPressMsg{Code: tea.KeyPgUp}, err: context.Canceled},
		{name: "follow the bottom"},
		{name: "height growth near bottom", scroll: tea.MouseWheelMsg{Button: tea.MouseWheelUp}, layout: []tea.Msg{tea.WindowSizeMsg{Width: 80, Height: 30}}},
		{name: "theme height near bottom", scroll: tea.MouseWheelMsg{Button: tea.MouseWheelUp}, inputHeight: 6, layout: []tea.Msg{themeUpdate{theme: smallerInput}}},
		{name: "narrow reflow", scroll: tea.KeyPressMsg{Code: tea.KeyPgUp}, reflow: true, layout: []tea.Msg{tea.WindowSizeMsg{Width: 40, Height: 24}}},
		{name: "narrow and wide reflow", scroll: tea.KeyPressMsg{Code: tea.KeyPgUp}, reflow: true, layout: []tea.Msg{tea.WindowSizeMsg{Width: 40, Height: 24}, tea.WindowSizeMsg{Width: 80, Height: 24}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t.Context(), a, "test")
			if tc.inputHeight != 0 {
				themed := m.theme
				themed.InputHeight = tc.inputHeight
				m.applyTheme(themed)
			}
			m.busy = true
			m.input.SetValue("/mo") // Completion reappears and resizes on done.
			if tc.scroll != nil {
				next, _ := m.Update(tc.scroll)
				m = next.(model)
				if m.viewport.AtBottom() {
					t.Fatal("user could not scroll away from the bottom")
				}
			}
			before, _, _ := strings.Cut(ansi.Strip(m.viewport.View()), "\n")
			for _, layout := range tc.layout {
				next, _ := m.Update(layout)
				m = next.(model)
				if m.scroll.following {
					t.Fatal("layout enabled following without a user scroll")
				}
				if tc.reflow {
					marker := strings.Join(strings.Fields(before)[:3], " ")
					top, _, _ := strings.Cut(ansi.Strip(m.viewport.View()), "\n")
					if !strings.Contains(top, marker) {
						t.Fatalf("reflow lost %q: %q", marker, top)
					}
				}
			}
			offset := m.viewport.YOffset()
			top, _, _ := strings.Cut(ansi.Strip(m.viewport.View()), "\n")
			message := gai.Message{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(strings.Repeat("Accepted line\n", 20))}}
			for _, event := range []update{
				{event: agent.Event{Kind: agent.EventDelta, Text: strings.Repeat("Streamed line\n", 20)}},
				{event: agent.Event{Kind: agent.EventActivity, Text: "Running Starlark"}},
				{event: agent.Event{Kind: agent.EventUsage, Usage: &agent.Usage{}}},
				{event: agent.Event{Kind: agent.EventMessage, Message: &message}},
				{event: agent.Event{Kind: agent.EventDelta, Text: "More streamed text"}},
				{done: true, err: tc.err},
			} {
				next, _ := m.Update(event)
				m = next.(model)
				currentTop, _, _ := strings.Cut(ansi.Strip(m.viewport.View()), "\n")
				if tc.scroll == nil {
					if !m.viewport.AtBottom() {
						t.Fatal("new output stopped following the bottom")
					}
				} else if m.viewport.YOffset() != offset || currentTop != top {
					t.Fatalf("event %+v moved the reading position: offset %d -> %d", event, offset, m.viewport.YOffset())
				}
			}
			if m.input.Value() != "/mo" || m.completionHeight() == 0 {
				t.Fatal("completion lost the draft")
			}
			for i := 0; !m.viewport.AtBottom() && i < 100; i++ {
				next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
				m = next.(model)
			}
			if !m.viewport.AtBottom() {
				t.Fatal("could not scroll back to the bottom")
			}
			next, _ := m.Update(update{event: agent.Event{Kind: agent.EventDelta, Text: strings.Repeat("Follow again\n", 20)}})
			m = next.(model)
			if !m.viewport.AtBottom() {
				t.Fatal("scrolling to the bottom did not resume following")
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
