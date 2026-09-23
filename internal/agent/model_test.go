package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestModelSwitchPreservesStateAndChangesTurnsAndCompaction(t *testing.T) {
	const switchedModel = "second"
	const restoredOutput = "42\n"
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "models.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	call, err := gai.ToolCallBlock("first-call", "starlark_repl", map[string]any{"code": `load("tools.star", "counter"); saved = counter(); print(saved)`})
	if err != nil {
		t.Fatal(err)
	}
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Saved 41.")}}}, FinishReason: gai.EndTurn}},
	)
	calls := 0
	a, err := Open(t.Context(), Options{
		Config: config.Config{System: "Switch profiles", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 5}, Compaction: config.Compaction{Prompt: "Summarize"}},
		Model:  config.Model{Provider: codexProvider, ID: "first", ReasoningEffort: "low"}, Generator: gen, Store: store, CWD: dir,
		Tools: []repl.Tool{{Name: "counter", Execute: func(context.Context, map[string]any) (any, error) { calls++; return 41, nil }}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "save a value", nil); err != nil {
		t.Fatal(err)
	}
	interpreter, historySize := a.repl, len(store.Path())
	call, err = gai.ToolCallBlock("second-call", "starlark_repl", map[string]any{"code": `saved += 1; print(saved)`})
	if err != nil {
		t.Fatal(err)
	}
	next := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			if r.Model != switchedModel || r.Options[gai.GenerationOptionReasoningEffort] != "high" || len(r.Dialog) != 5 {
				return fmt.Errorf("wrong model, effort, or context: %s %v %d", r.Model, r.Options, len(r.Dialog))
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			last := r.Dialog[len(r.Dialog)-1]
			if r.Model != switchedModel || r.Options[gai.GenerationOptionReasoningEffort] != "high" || last.Blocks[0].Content.String() != restoredOutput {
				return fmt.Errorf("lost model settings or interpreter value")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Now 42.")}}}, FinishReason: gai.EndTurn}},
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			if r.Model != switchedModel || r.Options[gai.GenerationOptionReasoningEffort] != "none" {
				return fmt.Errorf("compaction ignored updated effort")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Saved is 42.")}}}, FinishReason: gai.EndTurn}},
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			if _, exists := r.Options[gai.GenerationOptionReasoningEffort]; exists {
				return fmt.Errorf("empty effort was not omitted")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Still 42.")}}}, FinishReason: gai.EndTurn}},
	)
	profile := config.Model{Provider: "responses", ID: switchedModel, ReasoningEffort: "medium"}
	if err := a.SetModel(profile, next); err != nil {
		t.Fatal(err)
	}
	if err := a.SetReasoningEffort("high"); err != nil {
		t.Fatal(err)
	}
	if a.repl != interpreter || len(store.Path()) != historySize || len(a.Messages()) != 4 || calls != 1 {
		t.Fatal("changing settings altered durable state")
	}
	before := a.Model()
	if a.SetModel(profile, nil) == nil || a.SetReasoningEffort("bogus") == nil || a.Model() != before {
		t.Fatal("invalid changes did not leave the current model intact")
	}
	if err := a.Prompt(t.Context(), "increment it", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.SetReasoningEffort("none"); err != nil {
		t.Fatal(err)
	}
	if err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.SetReasoningEffort(""); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(t.Context(), "continue", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || a.repl != interpreter {
		t.Fatal("model switch replayed host effects or replaced the interpreter")
	}
}
