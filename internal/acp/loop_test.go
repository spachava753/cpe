package acp

import (
	"context"
	"math"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

// fakeCostAdder is an in-memory storage.ACPSessionCostAdder for unit tests.
type fakeCostAdder struct {
	totals map[acp.SessionId]float64
}

func (f *fakeCostAdder) AddACPSessionCost(_ context.Context, sessionID acp.SessionId, costUSD float64) (float64, error) {
	if f.totals == nil {
		f.totals = make(map[acp.SessionId]float64)
	}
	f.totals[sessionID] += costUSD
	return f.totals[sessionID], nil
}

func TestLoopUsageSessionUpdate(t *testing.T) {
	tests := []struct {
		name     string
		model    config.Model
		metadata gai.Metadata
		wantUsed int
		wantCost *float64
	}{
		{
			name: "cache read with input cost",
			model: config.Model{
				ContextWindow:       200,
				InputCostPerMillion: new(2.0),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:     100,
				gai.UsageMetricCacheReadTokens: 40,
			},
			wantUsed: 100,
			wantCost: new(0.00012),
		},
		{
			name: "cache read and write with input cost",
			model: config.Model{
				ContextWindow:            200,
				InputCostPerMillion:      new(2.0),
				CacheReadCostPerMillion:  new(0.5),
				CacheWriteCostPerMillion: new(1.0),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      100,
				gai.UsageMetricCacheReadTokens:  40,
				gai.UsageMetricCacheWriteTokens: 10,
			},
			wantUsed: 100,
			wantCost: new(0.00013),
		},
		{
			name: "no model pricing omits cost",
			model: config.Model{
				ContextWindow: 200,
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens:      100,
				gai.UsageMetricGenerationTokens: 25,
				gai.UsageMetricCacheReadTokens:  40,
			},
			wantUsed: 125,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Loop{Cfg: config.Config{Model: tt.model}, CostAdder: &fakeCostAdder{}}
			update, ok, err := l.usageSessionUpdate(t.Context(), "test-session", tt.metadata)
			if err != nil {
				t.Fatalf("usageSessionUpdate() err = %v, want nil", err)
			}
			if !ok {
				t.Fatal("usageSessionUpdate() ok = false, want true")
			}
			if update.UsageUpdate == nil {
				t.Fatalf("usageSessionUpdate().UsageUpdate is nil")
			}
			usage := update.UsageUpdate
			if usage.Size != int(tt.model.ContextWindow) {
				t.Fatalf("Size = %d, want %d", usage.Size, tt.model.ContextWindow)
			}
			if usage.Used != tt.wantUsed {
				t.Fatalf("Used = %d, want %d", usage.Used, tt.wantUsed)
			}

			if tt.wantCost == nil {
				if usage.Cost != nil {
					t.Fatalf("Cost = %#v, want nil", usage.Cost)
				}
				return
			}
			if usage.Cost == nil {
				t.Fatal("Cost is nil")
			}
			if usage.Cost.Currency != "USD" {
				t.Fatalf("Cost.Currency = %q, want USD", usage.Cost.Currency)
			}
			if math.Abs(usage.Cost.Amount-*tt.wantCost) > 0.0000000001 {
				t.Fatalf("Cost.Amount = %.12f, want %.12f", usage.Cost.Amount, *tt.wantCost)
			}
		})
	}
}

// TestLoopUsageSessionUpdateCostAccumulatesAcrossLoops verifies that the
// reported cost is the session's cumulative total even when the Loop is
// recreated (new prompt, model switch, or process restart) since the total
// is persisted through the CostAdder rather than held in Loop memory.
func TestLoopUsageSessionUpdateCostAccumulatesAcrossLoops(t *testing.T) {
	adder := &fakeCostAdder{}
	sessionID := acp.SessionId("test-session")
	metadata := gai.Metadata{
		gai.UsageMetricInputTokens:      100,
		gai.UsageMetricGenerationTokens: 50,
	}

	first := Loop{
		Cfg: config.Config{Model: config.Model{
			ContextWindow:        200,
			InputCostPerMillion:  new(2.0),
			OutputCostPerMillion: new(4.0),
		}},
		CostAdder: adder,
	}
	update, ok, err := first.usageSessionUpdate(t.Context(), sessionID, metadata)
	if err != nil || !ok || update.UsageUpdate == nil || update.UsageUpdate.Cost == nil {
		t.Fatalf("first usageSessionUpdate() = %#v, %v, %v", update, ok, err)
	}
	// 100 * 2/1M + 50 * 4/1M
	wantFirst := 0.0004
	if math.Abs(update.UsageUpdate.Cost.Amount-wantFirst) > 0.0000000001 {
		t.Fatalf("first Cost.Amount = %.12f, want %.12f", update.UsageUpdate.Cost.Amount, wantFirst)
	}

	// a new Loop with different model pricing simulates a model switch,
	// which discards the previous runtime and its Loop
	second := Loop{
		Cfg: config.Config{Model: config.Model{
			ContextWindow:        200,
			InputCostPerMillion:  new(1.0),
			OutputCostPerMillion: new(1.0),
		}},
		CostAdder: adder,
	}
	update, ok, err = second.usageSessionUpdate(t.Context(), sessionID, metadata)
	if err != nil || !ok || update.UsageUpdate == nil || update.UsageUpdate.Cost == nil {
		t.Fatalf("second usageSessionUpdate() = %#v, %v, %v", update, ok, err)
	}
	// previous total plus 100 * 1/1M + 50 * 1/1M
	wantSecond := wantFirst + 0.00015
	if math.Abs(update.UsageUpdate.Cost.Amount-wantSecond) > 0.0000000001 {
		t.Fatalf("second Cost.Amount = %.12f, want %.12f", update.UsageUpdate.Cost.Amount, wantSecond)
	}

	// a different session must not see this session's cost
	other := Loop{Cfg: second.Cfg, CostAdder: adder}
	update, ok, err = other.usageSessionUpdate(t.Context(), "other-session", metadata)
	if err != nil || !ok || update.UsageUpdate == nil || update.UsageUpdate.Cost == nil {
		t.Fatalf("other session usageSessionUpdate() = %#v, %v, %v", update, ok, err)
	}
	if math.Abs(update.UsageUpdate.Cost.Amount-0.00015) > 0.0000000001 {
		t.Fatalf("other session Cost.Amount = %.12f, want %.12f", update.UsageUpdate.Cost.Amount, 0.00015)
	}
}

func TestLoopEffectiveGenOpts(t *testing.T) {
	tests := []struct {
		name        string
		modelType   string
		cfgParams   *gai.GenOpts
		override    *gai.GenOpts
		want        *gai.GenOpts
		wantSummary any
	}{
		{
			name: "both nil returns nil",
		},
		{
			name:      "config params apply when no override",
			cfgParams: &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "low"},
			want:      &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "low"},
		},
		{
			name:     "override applies when no config params",
			override: &gai.GenOpts{ThinkingBudget: "high"},
			want:     &gai.GenOpts{ThinkingBudget: "high"},
		},
		{
			name:      "override wins over config without dropping config fields",
			cfgParams: &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "low"},
			override:  &gai.GenOpts{ThinkingBudget: "high"},
			want:      &gai.GenOpts{MaxGenerationTokens: new(32000), ThinkingBudget: "high"},
		},
		{
			name:        "responses thinking override requests detailed summary",
			modelType:   "responses",
			override:    &gai.GenOpts{ThinkingBudget: "high"},
			want:        &gai.GenOpts{ThinkingBudget: "high"},
			wantSummary: responses.ReasoningSummaryDetailed,
		},
		{
			name:        "responses thinking config requests detailed summary",
			modelType:   "responses",
			cfgParams:   &gai.GenOpts{ThinkingBudget: "low"},
			want:        &gai.GenOpts{ThinkingBudget: "low"},
			wantSummary: responses.ReasoningSummaryDetailed,
		},
		{
			name:      "responses without thinking omits summary request",
			modelType: "responses",
			override:  &gai.GenOpts{MaxGenerationTokens: new(32000)},
			want:      &gai.GenOpts{MaxGenerationTokens: new(32000)},
		},
		{
			name:      "responses preserves explicit summary request",
			modelType: "responses",
			override: &gai.GenOpts{
				ThinkingBudget: "high",
				ExtraArgs: map[string]any{
					gai.ResponsesThoughtSummaryDetailParam: responses.ReasoningSummaryConcise,
				},
			},
			want:        &gai.GenOpts{ThinkingBudget: "high"},
			wantSummary: responses.ReasoningSummaryConcise,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Loop{Cfg: config.Config{
				Model:            config.Model{Type: tt.modelType},
				GenerationParams: tt.cfgParams,
			}}
			got := l.effectiveGenOpts(tt.override)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("effectiveGenOpts() = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("effectiveGenOpts() is nil")
			}
			if got.ThinkingBudget != tt.want.ThinkingBudget {
				t.Fatalf("ThinkingBudget = %q, want %q", got.ThinkingBudget, tt.want.ThinkingBudget)
			}
			if tt.want.MaxGenerationTokens == nil {
				if got.MaxGenerationTokens != nil {
					t.Fatalf("MaxGenerationTokens = %d, want nil", *got.MaxGenerationTokens)
				}
			} else {
				if got.MaxGenerationTokens == nil {
					t.Fatal("MaxGenerationTokens is nil")
				}
				if *got.MaxGenerationTokens != *tt.want.MaxGenerationTokens {
					t.Fatalf("MaxGenerationTokens = %d, want %d", *got.MaxGenerationTokens, *tt.want.MaxGenerationTokens)
				}
			}

			gotSummary, gotSummaryOK := got.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]
			if tt.wantSummary == nil {
				if gotSummaryOK {
					t.Fatalf("responses thought summary = %#v, want unset", gotSummary)
				}
				return
			}
			if !gotSummaryOK {
				t.Fatalf("responses thought summary is unset, want %#v", tt.wantSummary)
			}
			if gotSummary != tt.wantSummary {
				t.Fatalf("responses thought summary = %#v, want %#v", gotSummary, tt.wantSummary)
			}
		})
	}
}

func TestLoopEffectiveGenOptsDoesNotMutateInputExtraArgs(t *testing.T) {
	extraArgs := map[string]any{"custom": "value"}
	override := &gai.GenOpts{
		ThinkingBudget: "high",
		ExtraArgs:      extraArgs,
	}
	l := Loop{Cfg: config.Config{Model: config.Model{Type: "responses"}}}

	got := l.effectiveGenOpts(override)
	if got == nil {
		t.Fatal("effectiveGenOpts() is nil")
	}
	if got.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam] != responses.ReasoningSummaryDetailed {
		t.Fatalf(
			"responses thought summary = %#v, want %#v",
			got.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam],
			responses.ReasoningSummaryDetailed,
		)
	}
	if _, ok := override.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]; ok {
		t.Fatalf("override ExtraArgs was mutated: %#v", override.ExtraArgs)
	}
	if override.ExtraArgs["custom"] != "value" {
		t.Fatalf("override custom ExtraArgs = %#v, want value", override.ExtraArgs["custom"])
	}
}
