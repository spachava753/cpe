package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestAgentSingleToolDurableHooksResumeBranchAndCompaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	store, err := session.Open(path, dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{System: "Exercise durable model and tool turns.", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 5}, Compaction: config.Compaction{Prompt: "Summarize"}}
	call, err := gai.ToolCallBlock("call1", "starlark_repl", map[string]any{codeParameter: `load("tools.star", "counter"); x = counter(); print(x)`})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	tool := repl.Tool{Name: "counter", Execute: func(context.Context, map[string]any) (any, error) {
		// The assistant call and host intent must already be durable.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(data), `"role":2`) || !strings.Contains(string(data), `"type":"host_call"`) {
			return nil, errors.New("host executed before persistence")
		}
		calls++
		return 42, nil
	}}
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			if len(r.Tools) != 1 || r.Tools[0].Name != "starlark_repl" {
				return fmt.Errorf("exposed tools: %v", r.Tools)
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			last := r.Dialog[len(r.Dialog)-1]
			if last.Role != gai.ToolResult || last.Blocks[0].Content.String() != "42\n" {
				return fmt.Errorf("unexpected result %+v", last)
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Done")}}}, FinishReason: gai.EndTurn}},
	)
	opts := Options{Config: cfg, Model: config.Model{ID: "durability-test"}, Generator: gen, Store: store, CWD: dir, Tools: []repl.Tool{tool}}
	a, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(t.Context(), "compute", nil); err != nil {
		t.Fatal(err)
	}
	checkpoints := a.Checkpoints()
	first := checkpoints[len(checkpoints)-1].ID
	if calls != 1 || len(a.Messages()) != 4 {
		t.Fatalf("calls=%d messages=%d", calls, len(a.Messages()))
	}
	_ = a.Close()
	_ = store.Close()
	store, err = session.Open(path, dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	opts.Store = store
	opts.Tools[0].Execute = func(context.Context, map[string]any) (any, error) { t.Fatal("replayed injected tool"); return nil, nil }
	call2, err := gai.ToolCallBlock("call2", "starlark_repl", map[string]any{codeParameter: "x += 1; print(x)"})
	if err != nil {
		t.Fatal(err)
	}
	opts.Generator = agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call2}}}, FinishReason: gai.ToolUse}},
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("43")}}}, FinishReason: gai.EndTurn}},
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("We computed x = 43.")}}}, FinishReason: gai.EndTurn}},
	)
	a, err = Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "increment", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(a.Messages()) != 1 {
		t.Fatal("compaction did not replace context")
	}
	// Restoring the compacted session still rebuilds every committed REPL chunk.
	if err := a.restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := a.repl.Eval(t.Context(), "check", `print(x)`)
	if err != nil || result.Output != "43\n" {
		t.Fatal(result, err)
	}
	if err := a.Branch(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	result, err = a.repl.Eval(t.Context(), "branch-check", `print(x)`)
	if err != nil || result.Output != "42\n" {
		t.Fatal(result, err)
	}
	if len(a.Messages()) != 4 {
		t.Fatal("branch context wrong")
	}
}

func TestInterruptedToolResultReconciliation(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	call, err := gai.ToolCallBlock("pending", "starlark_repl", map[string]any{codeParameter: "x = 7; print(x)"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("message", encodeMessage(gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("start")}})); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("message", encodeMessage(gai.Message{Role: gai.Assistant, Blocks: []gai.Block{call}})); err != nil {
		t.Fatal(err)
	}
	r, err := repl.New(t.Context(), repl.Options{Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Eval(t.Context(), "pending", "x = 7; print(x)")
	if err != nil || result.Error != "" {
		t.Fatal(result, err)
	}
	_ = r.Close()
	a, err := Open(t.Context(), Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "1s"}}, Model: config.Model{ID: "reconciliation-test"}, Generator: agenttest.NewScriptedGenerator(), Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	last := a.Messages()[len(a.Messages())-1]
	if last.Role != gai.ToolResult || last.Blocks[0].ID != "pending" || last.Blocks[0].Content.String() != "7\n" {
		t.Fatal(last)
	}
	before := len(store.Entries())
	if err := a.restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(store.Entries()) != before {
		t.Fatal("duplicate reconciliation")
	}

	// A provider can reuse an ID once compaction removes its old tool call.
	summary := gai.Message{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("Summary")}}
	if _, err := store.Append("compaction", encodeMessage(summary)); err != nil {
		t.Fatal(err)
	}
	reused, err := gai.ToolCallBlock("pending", "starlark_repl", map[string]any{codeParameter: "x = 8; print(x)"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("message", encodeMessage(gai.Message{Role: gai.Assistant, Blocks: []gai.Block{reused}})); err != nil {
		t.Fatal(err)
	}
	if err := a.restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	last = a.Messages()[len(a.Messages())-1]
	if !last.ToolResultError || !strings.Contains(last.Blocks[0].Content.String(), "interrupted") {
		t.Fatalf("borrowed an obsolete tool outcome: %+v", last)
	}
}

func TestMessageCodecPreservesProviderReplayData(t *testing.T) {
	input := gai.Message{Role: gai.Assistant, ExtraFields: map[string]any{"phase": "commentary"}, Blocks: []gai.Block{{ID: "reasoning-id", BlockType: gai.Thinking, Content: gai.Str("thought"), ExtraFields: map[string]any{"signature": "signed"}}, gai.ImageBlock([]byte{0, 255}, "image/png")}}
	data, err := json.Marshal(encodeMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExtraFields["phase"] != "commentary" || result.Blocks[0].ExtraFields["signature"] != "signed" || result.Blocks[1].Content.String() != input.Blocks[1].Content.String() {
		t.Fatal(result)
	}
}

func TestPartialStreamIsNotDurable(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen := agenttest.NewScriptedStreamingGenerator(agenttest.StreamStep{Chunks: []gai.StreamChunk{{Block: gai.TextBlock("partial")}, {Err: errors.New("connection lost")}}})
	a, err := Open(t.Context(), Options{Config: config.Config{System: "Reconcile interrupted tool results.", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 5}}, Model: config.Model{ID: "stream-test"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "start", nil); err == nil {
		t.Fatal("expected stream failure")
	}
	if len(a.Messages()) != 1 || a.Messages()[0].Role != gai.User {
		t.Fatal("partial output persisted")
	}
}

func TestSyntheticResultBeforeExecutedTool(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	invalid, err := gai.ToolCallBlock("invalid", "unknown", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := gai.ToolCallBlock("valid", "starlark_repl", map[string]any{codeParameter: "print(42)"})
	if err != nil {
		t.Fatal(err)
	}
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{invalid, valid}}}, FinishReason: gai.ToolUse}},
		agenttest.GenerateStep{Check: func(r gai.GenerationRequest) error {
			if len(r.Dialog) != 4 || !r.Dialog[2].ToolResultError || r.Dialog[3].ToolResultError {
				return errors.New("incorrect batch results")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Done")}}}, FinishReason: gai.EndTurn}},
	)
	a, err := Open(t.Context(), Options{Config: config.Config{System: "Test partial stream failure.", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 32000, MaxRounds: 5}}, Model: config.Model{ID: "batch-test"}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(t.Context(), "try tools", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.Messages()) != 5 {
		t.Fatalf("messages = %d", len(a.Messages()))
	}
}
