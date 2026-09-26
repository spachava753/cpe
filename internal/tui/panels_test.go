package tui

import (
	"fmt"
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

func TestReadOnlyPanels(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var history strings.Builder
	for i := range 100 {
		fmt.Fprintf(&history, "History line %03d %s\n", i, strings.Repeat("x", 55))
	}
	gen := agenttest.NewScriptedGenerator(agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock(history.String())}}}, FinishReason: gai.EndTurn}})
	a, err := agent.Open(t.Context(), agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s", MaxRounds: 3}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "seed", nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, command, overlay                 string
		busy, follow, resize, nested, controlC bool
	}{
		{name: "usage idle", command: usageCommand},
		{name: "usage during generation", command: usageCommand, busy: true},
		{name: "usage follows new output", command: usageCommand, busy: true, follow: true},
		{name: "usage resize preserves reading anchor", command: usageCommand, busy: true, resize: true},
		{name: "tree", command: treeCommand},
		{name: "tree during generation", command: treeCommand, busy: true},
		{name: "queue during generation", command: queueCommand, busy: true},
		{name: "switch panels", command: usageCommand, nested: true},
		{name: "completion dismisses first", command: usageCommand, overlay: "completion"},
		{name: "busy completion dismisses first", command: usageCommand, overlay: "completion", busy: true},
		{name: "picker dismisses first", command: usageCommand, overlay: "picker", busy: true},
		{name: "default save cancels first", command: usageCommand, overlay: "save", busy: true},
		{name: "control C still cancels work", command: usageCommand, busy: true, controlC: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t.Context(), a, "fixture")
			m.profiles = map[string]config.Model{"fixture": a.Model()}
			m.busy = tc.busy
			canceled, saves := 0, 0
			m.cancel = func() { canceled++ }
			if tc.busy {
				m.queuedCommands = []queuedCommand{{id: 1, request: commandRequest{argument: "pending"}}}
			}
			if !tc.follow {
				m.viewport.SetYOffset(30)
				m.scroll.following = false
			}
			beforeLine, _, _ := strings.Cut(ansi.Strip(m.viewport.View()), "\n")
			beforeEntries := len(store.Entries())
			m.dispatchCommand(commandRequest{kind: commandKind(tc.command)})
			if tc.nested {
				m.dispatchCommand(commandRequest{kind: treeCommand})
				m.dispatchCommand(commandRequest{kind: queueCommand})
			}
			draft := "draft stays intact"
			m.input.SetValue(draft)
			if !strings.Contains(m.View().Content, "Esc conversation") {
				t.Fatal("missing panel exit help")
			}
			if tc.resize {
				next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 28})
				m = next.(model)
			}
			if tc.busy {
				next, _ := m.Update(update{event: agent.Event{Kind: agent.EventDelta, Text: "latest provisional output"}})
				m = next.(model)
			}
			switch tc.overlay {
			case "completion":
				draft = "/mo"
				m.input.SetValue(draft)
				m.syncCompletion()
				if !strings.Contains(m.completionHelp(), "Esc dismiss") {
					t.Fatal("incorrect completion help")
				}
			case "picker":
				m.configureModel(modelCommand, "")
			case "save":
				m.savingDefault = &defaultSave{cancel: func() { saves++ }}
			}
			view := ansi.Strip(m.View().Content)
			if tc.busy && tc.overlay != "picker" {
				if tc.overlay == "save" {
					if !strings.Contains(view, "Esc to cancel save") {
						t.Fatal("missing default-save cancellation guidance")
					}
				} else if strings.Contains(view, "Esc to cancel") || !tc.resize && !strings.Contains(view, "Ctrl+C to cancel") {
					t.Fatalf("panel cancellation guidance disagrees with key routing: %s", view)
				}
			}
			key := tea.KeyPressMsg{Code: tea.KeyEsc, Mod: tea.ModCapsLock}
			if tc.controlC {
				key = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
			}
			next, _ := m.Update(key)
			m = next.(model)
			if tc.controlC {
				if canceled != 1 || len(m.queuedCommands) != 0 || m.panel.kind == conversationPanel {
					t.Fatal("control C changed cancellation contract")
				}
			} else {
				if canceled != 0 || tc.busy && len(m.queuedCommands) != 1 {
					t.Fatal("Escape canceled work/queue")
				}
				if tc.overlay != "" {
					if m.panel.kind == conversationPanel {
						t.Fatal("overlay escape closed underlying panel")
					}
					if tc.overlay == "save" {
						if saves != 1 {
							t.Fatal("save not canceled")
						}
						m.savingDefault = nil
					}
					next, _ = m.Update(key)
					m = next.(model)
				}
				if m.panel.kind != conversationPanel || strings.Contains(m.View().Content, "Session usage") {
					t.Fatal("panel did not close")
				}
				if tc.follow {
					if !m.scroll.following || !m.viewport.AtBottom() || !strings.Contains(m.viewport.View(), "latest provisional output") {
						t.Fatal("lost follow mode/latest output")
					}
				} else if m.scroll.following || !strings.Contains(ansi.Strip(m.viewport.View()), beforeLine[:16]) {
					t.Fatalf("reading anchor changed: wanted %q, got %q", beforeLine, m.viewport.View())
				}
			}
			if m.input.Value() != draft || len(store.Entries()) != beforeEntries {
				t.Fatal("Escape altered draft/history")
			}
		})
	}
}
