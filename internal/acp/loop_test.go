package acp

import (
	"math"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

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
			l := Loop{Cfg: config.Config{Model: tt.model}}
			update, ok := l.usageSessionUpdate(tt.metadata)
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
