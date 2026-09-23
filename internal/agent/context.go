package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spachava753/gai"
	gaiagent "github.com/spachava753/gai/agent"
)

// estimateTokens includes instructions, tools, dialog, and protocol overhead.
// UTF-8 bytes / 3 is a conservative text heuristic, not a provider tokenizer.
// It is deliberately independent of cumulative billed usage across requests.
func estimateTokens(req gai.GenerationRequest) int {
	data, _ := json.Marshal(req)
	return (len(data)+2)/3 + 32
}

type contextSample struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Estimate int    `json:"estimate"`
	Input    int    `json:"input"`
}

func (a *Agent) estimateContext(req gai.GenerationRequest) int {
	estimate := estimateTokens(req)
	if sample := a.contextSample; sample != nil && sample.Provider == a.opts.Model.Provider && sample.Model == req.Model {
		// Use measured input plus estimated growth when that exceeds the local
		// estimate. Compaction and branching reset/select the relevant sample.
		estimate = max(estimate, sample.Input+max(0, estimate-sample.Estimate))
	}
	return estimate
}

func (a *Agent) checkContext(req gai.GenerationRequest) error {
	if limit := a.opts.Model.ContextWindow; limit > 0 && a.estimateContext(req) > limit {
		return fmt.Errorf("estimated input exceeds context_window (%d tokens); reduce the input or select a larger-budget profile before /compact", limit)
	}
	return nil
}

func (a *Agent) prepareContext(ctx context.Context, req gai.GenerationRequest) (gaiagent.PrepareDialogDecision, error) {
	data, err := json.Marshal(req.Dialog)
	if err != nil {
		return gaiagent.PrepareDialogDecision{}, err
	}
	characters := a.opts.Config.Compaction.MaxCharacters
	window := a.opts.Model.ContextWindow
	needsCompaction := window == 0 && characters > 0 && len(data) > characters
	if window > 0 && a.estimateContext(req) >= window-window/10 {
		needsCompaction = true
	}
	if needsCompaction && !a.compacted {
		if err := a.Compact(ctx); err != nil {
			return gaiagent.PrepareDialogDecision{}, err
		}
		a.compacted = true
		req.Dialog = a.dialog
		return gaiagent.PrepareDialogDecision{Dialog: a.dialog}, a.checkContext(req)
	}
	return gaiagent.PrepareDialogDecision{}, a.checkContext(req)
}

// ContextEstimate returns an approximate current input size including system and
// tool instructions. Read it only between operations. Billing uses provider
// usage instead; this number is solely a working-context estimate.
func (a *Agent) ContextEstimate() int {
	return a.estimateContext(gai.GenerationRequest{Model: a.opts.Model.ID,
		Instructions: a.instructions(), Tools: []gai.Tool{replDefinition()},
		Dialog: a.dialog, Options: a.generationOptions()})
}
