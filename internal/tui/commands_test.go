package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/skills"
)

func TestParseCommand(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		literal    bool
		want       commandRequest
		invalid    bool
	}{
		{name: "ordinary draft", text: "hello /model", want: commandRequest{argument: "hello /model"}},
		{name: "model argument", text: "/model team/long name", want: commandRequest{modelCommand, "team/long name"}},
		{name: "login method", text: loginDeviceCommand, want: commandRequest{loginCommand, openAIDeviceLogin}},
		{name: "skill arguments", text: "/skill:review code\nwith details", want: commandRequest{skillCommand, "/skill:review code\nwith details"}},
		{name: "branch", text: "/branch abc", want: commandRequest{branchCommand, "abc"}},
		{name: "missing branch", text: branchCommand, invalid: true},
		{name: "clear queue", text: "/queue clear", want: commandRequest{queueCommand, "clear"}},
		{name: "new session", text: sessionNewCommand, want: commandRequest{kind: sessionNewCommand}},
		{name: "session picker", text: sessionListCommand, want: commandRequest{kind: sessionListCommand}},
		{name: "invalid session action", text: "/session unknown", invalid: true},
		{name: "rename title", text: "/session rename  Fix auth for Copilot", want: commandRequest{sessionRenameCommand, "Fix auth for Copilot"}},
		{name: "rename needs title", text: "/session rename", invalid: true},
		{name: "session action extra argument", text: "/session new extra", invalid: true},
		{name: "unknown queue action", text: "/queue next", invalid: true},
		{name: "unknown", text: "/unknown", invalid: true},
		{name: "dismissed unknown", text: "/unknown", literal: true, want: commandRequest{argument: "/unknown"}},
		{name: "dismissed known", text: compactCommand, literal: true, want: commandRequest{kind: compactCommand}},
		{name: "bad arguments", text: "/help something", invalid: true},
		{name: "empty", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCommand(tc.text, tc.literal)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("request=%+v error=%v", got, err)
			}
		})
	}
}

func TestCommandsDuringWork(t *testing.T) {
	for _, tc := range []struct {
		name, command                                               string
		picker, save, pendingModel, cancel, clear, quit, holdPicker bool
		followLogin                                                 bool
		queued, calls, loginCalls                                   int
	}{
		{name: "help", command: helpCommand, calls: 1},
		{name: "usage", command: usageCommand, calls: 1},
		{name: "session", command: sessionCommand, calls: 1},
		{name: "tree", command: treeCommand, calls: 1},
		{name: "theme", command: "/theme nord", calls: 1},
		{name: "theme picker", command: themeCommand, picker: true, calls: 1},
		{name: "reasoning", command: "/reasoning high", calls: 1},
		{name: "reasoning picker", command: reasoningCommand, picker: true, calls: 1},
		{name: "reasoning default save", command: reasoningCommand, picker: true, save: true, calls: 1},
		{name: "reasoning on pending model", command: "/reasoning high", pendingModel: true, calls: 1},
		{name: "reasoning save on pending model", command: reasoningCommand, picker: true, save: true, pendingModel: true, calls: 1},
		{name: "compact", command: compactCommand, queued: 1, calls: 2},
		{name: "compact waits for picker", command: compactCommand, queued: 1, calls: 2, holdPicker: true},
		{name: "branch", command: branchCommand, queued: 1, calls: 1},
		{name: "failed branch followed by successful login", command: "/branch does-not-exist", queued: 2, calls: 1, loginCalls: 1, followLogin: true},
		{name: "login", command: loginDeviceCommand, queued: 1, calls: 1, loginCalls: 1},
		{name: "login picker", command: loginCommand, picker: true, queued: 1, calls: 1, loginCalls: 1},
		{name: "cancel pending", command: compactCommand, queued: 1, calls: 1, cancel: true},
		{name: "clear pending", command: compactCommand, queued: 1, calls: 1, clear: true},
		{name: "quit pending", command: compactCommand, queued: 1, calls: 1, quit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "commands.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			profiles := map[string]config.Model{"alpha": {Provider: "codex", ID: "a"}, "beta": {Provider: "codex", ID: "b"}}
			data, err := json.Marshal(map[string]any{"default_model": "alpha", "models": profiles})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "system.md"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			gen := &waitingGenerator{started: make(chan struct{}), finish: make(chan error, 1)}
			a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}, Compaction: config.Compaction{Prompt: "summarize"}}, Model: profiles["alpha"], Generator: gen, Store: store, CWD: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "alpha")
			m.configDir, m.themeDir, m.profiles = dir, dir, profiles
			m.newGenerator = func(context.Context, config.Model) (gai.Generator, error) { return gen, nil }
			var logins atomic.Int32
			m.login = func(context.Context, string, func(string)) error { logins.Add(1); return nil }
			text := tc.command
			if text == branchCommand {
				text += " " + a.Checkpoints()[0].ID
			}
			m.start("wait")
			defer func() {
				if m.busy {
					m.cancel()
					for u := range m.events {
						if u.done {
							break
						}
					}
				}
				if m.savingDefault != nil {
					m.savingDefault.stop()
				}
			}()
			<-gen.started
			m.input.SetValue("/")
			m.syncCompletion()
			if len(m.completion.matches) != len(m.commands) {
				t.Fatal("active work hid commands")
			}
			if tc.pendingModel {
				m.switchModel("beta")
			}
			m.input.SetValue(text)
			m.syncCompletion()
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			var cmd tea.Cmd
			m = next.(model)
			if tc.followLogin {
				m.input.SetValue(loginDeviceCommand)
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
			}
			if tc.picker {
				if m.picker == nil {
					t.Fatal("active picker unavailable")
				}
				if tc.command == reasoningCommand {
					for i, item := range m.picker.list.Items() {
						if item.(choice).value == "high" {
							m.picker.list.Select(i)
						}
					}
				}
				key := tea.KeyPressMsg{Code: tea.KeyEnter}
				if tc.save {
					key = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
				}
				next, cmd = m.Update(key)
				m = next.(model)
				if tc.save {
					for _, msg := range commandMessages(cmd) {
						next, _ = m.Update(msg)
						m = next.(model)
					}
				}
			}
			if !m.busy || gen.calls.Load() != 1 || len(m.queuedCommands) != tc.queued || logins.Load() != 0 {
				t.Fatalf("work overlapped or submission was ignored: notice=%s queue=%+v", m.notice, m.queuedCommands)
			}
			if tc.queued > 0 && !strings.Contains(m.View().Content, "queued") {
				t.Fatal("queue is invisible")
			}
			if tc.clear {
				m.input.SetValue("/queue clear")
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
				if len(m.queuedCommands) != 0 || !m.busy {
					t.Fatal("clear did not preserve active work")
				}
			}
			if tc.holdPicker {
				m.configureModel(modelCommand, "")
			}
			m.input.SetValue("unsent draft")
			if tc.cancel {
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = next.(model)
			} else if tc.quit {
				m.input.SetValue(quitCommand)
				next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
				if !m.quitting || len(m.queuedCommands) != 0 || cmd == nil {
					t.Fatal("quit retained queued work")
				}
				m.cancel()
			} else {
				gen.finish <- nil
			}
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case event := <-m.events:
					next, _ = m.Update(event)
					m = next.(model)
				case <-deadline:
					t.Fatal("work did not settle")
				}
			}
			if tc.holdPicker {
				if len(m.queuedCommands) != 1 || gen.calls.Load() != 1 {
					t.Fatal("queue ran beneath picker")
				}
				next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = next.(model)
				for m.busy {
					select {
					case event := <-m.events:
						next, _ = m.Update(event)
						m = next.(model)
					case <-deadline:
						t.Fatal("queued work did not settle")
					}
				}
			}
			if gen.calls.Load() != int32(tc.calls) || logins.Load() != int32(tc.loginCalls) || len(m.queuedCommands) != 0 {
				t.Fatalf("calls=%d logins=%d queued=%d", gen.calls.Load(), logins.Load(), len(m.queuedCommands))
			}
			if !tc.quit && m.input.Value() != "unsent draft" {
				t.Fatal("completion overwrote unrelated draft")
			}
			if tc.followLogin {
				if len(m.operationErrors) != 1 || !strings.Contains(m.View().Content, "Error: 1 failed") || !strings.Contains(m.notice, "Signed in") {
					t.Fatalf("failure lost after successful queued work: %s", m.View().Content)
				}
				m.dispatchCommand(commandRequest{kind: queueCommand})
				if !strings.Contains(m.View().Content, "Failed operations") || !strings.Contains(m.View().Content, "does-not-exist") {
					t.Fatalf("failure not inspectable: %s", m.View().Content)
				}
				m.dispatchCommand(commandRequest{queueCommand, "clear"})
				if len(m.operationErrors) != 1 || !strings.Contains(m.View().Content, "does-not-exist") {
					t.Fatal("clearing pending commands erased failure")
				}
			}
			if strings.HasPrefix(tc.command, reasoningCommand) {
				target := "alpha"
				if tc.pendingModel {
					target = "beta"
				}
				if m.name != target || a.Model().ReasoningEffort != "high" {
					t.Fatalf("reasoning target=%s/%s", m.name, a.Model().ReasoningEffort)
				}
				if tc.save {
					bytes, err := os.ReadFile(filepath.Join(dir, "config.json"))
					if err != nil {
						t.Fatal(err)
					}
					var cfg config.Config
					if err = json.Unmarshal(bytes, &cfg); err != nil {
						t.Fatal(err)
					}
					if cfg.DefaultModel != "alpha" || cfg.Models[target].ReasoningEffort != "high" {
						t.Fatal("reasoning save changed wrong default")
					}
				}
			}
			for _, message := range a.Messages() {
				for _, block := range message.Blocks {
					if block.Content != nil && strings.Contains(block.Content.String(), "unsent draft") {
						t.Fatal("draft leaked to history")
					}
				}
			}
		})
	}
}

func TestCommandQueueDuringTool(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name := "FIFO and reopen"
		if cancel {
			name = "cancellation clears queue"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "skills")
			if err := os.MkdirAll(filepath.Join(root, "review"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review changes\n---\nRead the files."), 0600); err != nil {
				t.Fatal(err)
			}
			catalog, warnings := skills.Discover(root)
			if len(warnings) > 0 {
				t.Fatal(warnings)
			}
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			started, finish := make(chan struct{}), make(chan struct{})
			var calls, requests atomic.Int32
			host := repl.Tool{Name: "hold", Execute: func(ctx context.Context, _ map[string]any) (any, error) {
				calls.Add(1)
				close(started)
				select {
				case <-finish:
					return 42, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			block, err := gai.ToolCallBlock("first", "starlark_repl", map[string]any{"code": "load(\"tools.star\", \"hold\"); answer = hold(); print(answer)"})
			if err != nil {
				t.Fatal(err)
			}
			steps := []agenttest.GenerateStep{
				{Check: func(gai.GenerationRequest) error { requests.Add(1); return nil }, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{block}}}, FinishReason: gai.ToolUse}},
				{Check: func(req gai.GenerationRequest) error {
					requests.Add(1)
					if req.Dialog[len(req.Dialog)-1].Role != gai.ToolResult {
						t.Error("old turn did not receive its tool result")
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("finished old turn")}}}, FinishReason: gai.EndTurn}},
			}
			for _, arg := range []string{"first", "second"} {
				steps = append(steps, agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					requests.Add(1)
					input := req.Dialog[len(req.Dialog)-1].Blocks[0].Content.String()
					if !strings.HasPrefix(input, "/skill:review "+arg+"\n\n") {
						t.Errorf("out-of-order skill: %s", input)
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("reviewed " + arg)}}}, FinishReason: gai.EndTurn}})
			}
			gen := agenttest.NewScriptedGenerator(steps...)
			opts := agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "5s", MaxRounds: 3, OutputLimit: 1024}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir, Skills: catalog, Tools: []repl.Tool{host}}
			a, err := agent.Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			m := newModel(t.Context(), a, "fixture")
			m.start("compute")
			defer func() {
				if m.busy {
					m.cancel()
					for event := range m.events {
						if event.done {
							break
						}
					}
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("host did not start")
			}
			for _, arg := range []string{"first", "second"} {
				m.input.SetValue("/skill:review " + arg)
				next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = next.(model)
			}
			if len(m.queuedCommands) != 2 || requests.Load() != 1 || calls.Load() != 1 {
				t.Fatal("queued command overlapped host execution")
			}
			m.input.SetValue("unsent draft")
			if cancel {
				next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
				m = next.(model)
			} else {
				close(finish)
			}
			deadline := time.After(5 * time.Second)
			for m.busy {
				select {
				case event := <-m.events:
					next, _ := m.Update(event)
					m = next.(model)
				case <-deadline:
					t.Fatal("work never settled")
				}
			}
			want := int32(4)
			if cancel {
				want = 1
			}
			if requests.Load() != want || calls.Load() != 1 || m.input.Value() != "unsent draft" || len(m.queuedCommands) != 0 {
				t.Fatalf("requests=%d host=%d draft=%q queue=%v", requests.Load(), calls.Load(), m.input.Value(), m.queuedCommands)
			}
			messages := a.Messages()
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			restored, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			opts.Store = restored
			reopened, err := agent.Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if calls.Load() != 1 || len(reopened.Messages()) != len(messages) {
				t.Fatal("reopen lost history or repeated host execution")
			}
		})
	}
}
