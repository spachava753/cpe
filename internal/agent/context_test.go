package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestContextBudgetCompactsAndRejectsOversizedInput(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "context.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
			if req.Instructions.Blocks[0].Content.String() != "Budget summary" {
				t.Error("normal request sent before compaction")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("A compact summary")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 100, gai.UsageMetricGenerationTokens: 10}}},
		agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
			if len(req.Dialog) != 1 || !strings.Contains(req.Dialog[0].Blocks[0].Content.String(), "A compact summary") {
				t.Error("uncompacted context sent")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Ready")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 20, gai.UsageMetricGenerationTokens: 2}}},
	)
	a, err := Open(t.Context(), Options{Config: config.Config{System: "Budget fixture", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 4}, Compaction: config.Compaction{Prompt: "Budget summary"}}, Model: config.Model{ID: "budget-model", ContextWindow: 10000}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.repl.Eval(t.Context(), "saved-before-compaction", "persistent = 42"); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(t.Context(), strings.Repeat("context ", 3000), nil); err != nil {
		t.Fatal(err)
	}
	if a.Usage().Requests != 2 || a.Usage().Total() != 132 {
		t.Fatalf("auto-compaction not accounted: %+v", a.Usage())
	}
	result, err := a.repl.Eval(t.Context(), "saved-after-compaction", "print(persistent)")
	if err != nil || strings.TrimSpace(result.Output) != "42" {
		t.Fatal("compaction changed REPL state", result, err)
	}
	before := a.Usage()
	if err := a.Prompt(t.Context(), strings.Repeat("too large ", 10000), nil); err == nil || !strings.Contains(err.Error(), "context_window") {
		t.Fatalf("oversized prompt not rejected: %v", err)
	}
	if a.Usage() != before {
		t.Fatal("oversized request reached provider")
	}
}

func TestContextEstimateUsesReportedInputAndResetsAfterCompaction(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Open(filepath.Join(dir, "calibrated.jsonl"), dir, repl.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen := agenttest.NewScriptedGenerator(
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("First response")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 9200, gai.UsageMetricGenerationTokens: 1}}},
		agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
			if req.Instructions.Blocks[0].Content.String() != "Calibrated summary" {
				t.Error("reported usage did not trigger compaction")
			}
			return nil
		}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Small summary")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 9300, gai.UsageMetricGenerationTokens: 10}}},
		agenttest.GenerateStep{Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("Continue")}}}, FinishReason: gai.EndTurn, UsageMetadata: gai.Metadata{gai.UsageMetricInputTokens: 1500, gai.UsageMetricGenerationTokens: 2}}},
	)
	a, err := Open(t.Context(), Options{Config: config.Config{System: "Estimate calibration", Agent: config.Agent{ToolTimeout: "1s", OutputLimit: 1024, MaxRounds: 3}, Compaction: config.Compaction{Prompt: "Calibrated summary", MaxCharacters: 1}}, Model: config.Model{Provider: codexProvider, ID: "calibration", ContextWindow: 10000}, Generator: gen, Store: store, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	// The per-model budget overrides the legacy one-character threshold.
	if err := a.Prompt(t.Context(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	if a.ContextEstimate() < 9200 {
		t.Fatal("reported input was ignored")
	}
	if err := a.Prompt(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	if a.Usage().Requests != 3 || a.ContextEstimate() >= 9000 {
		t.Fatalf("compaction did not reset context estimate: %d", a.ContextEstimate())
	}
}
