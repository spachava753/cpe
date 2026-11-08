package conversation

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/models"
)

func TestTokenUsage(t *testing.T) {
	for _, test := range []struct {
		previous  TokenUsage
		metadata  gai.Metadata
		name      string
		wantInput uint64
		wantError bool
	}{
		{
			name: "preserves exact JSON integers",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: json.Number("9007199254740993"),
			},
			wantInput: 9007199254740993,
		},
		{
			name: "adding does not mutate the earlier checkpoint",
			previous: TokenUsage{
				InputTokens: new(uint64(10)),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: 7,
			},
			wantInput: 17,
		},
		{
			name: "counter overflow",
			previous: TokenUsage{
				InputTokens: new(uint64(math.MaxUint64)),
			},
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: 1,
			},
			wantError: true,
		},
		{
			name: "total overflow",
			previous: TokenUsage{
				InputTokens: new(uint64(math.MaxUint64)),
			},
			metadata: gai.Metadata{
				gai.UsageMetricGenerationTokens: 1,
			},
			wantError: true,
		},
		{
			name: "negative token count",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: -1,
			},
			wantError: true,
		},
		{
			name: "fractional token count",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: json.Number("1.5"),
			},
			wantError: true,
		},
		{
			name: "wrong token type",
			metadata: gai.Metadata{
				gai.UsageMetricInputTokens: "12",
			},
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				before, err := json.Marshal(test.previous)
				if err != nil {
					t.Fatal(err)
				}
				delta, err := ParseTokenUsage(test.metadata)
				var actual TokenUsage
				if err == nil {
					actual, err = test.previous.Add(delta)
				}
				if (err != nil) != test.wantError {
					t.Fatalf(
						"usage error: %v",
						err,
					)
				}
				if err == nil && (actual.InputTokens == nil || *actual.InputTokens != test.wantInput) {
					t.Fatalf(
						"input tokens: %#v",
						actual,
					)
				}
				after, err := json.Marshal(test.previous)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("accumulation mutated an earlier checkpoint")
				}
			},
		)
	}
}

func TestTokenUsagePriced(t *testing.T) {
	priced := models.Model{
		Input:      2,
		Output:     10,
		CacheRead:  0.5,
		CacheWrite: 4,
	}
	for _, test := range []struct {
		usage    TokenUsage
		wantCost *float64
		name     string
		model    models.Model
	}{
		{
			name:  "cache breakdown is charged at its own rates",
			model: priced,
			usage: TokenUsage{
				InputTokens:      new(uint64(1_000_000)),
				OutputTokens:     new(uint64(100_000)),
				CacheReadTokens:  new(uint64(400_000)),
				CacheWriteTokens: new(uint64(100_000)),
				ReasoningTokens:  new(uint64(50_000)),
			},
			wantCost: new(1 + 1 + 0.2 + 0.4),
		},
		{
			name: "unset cache rates charge the input price",
			model: models.Model{
				Input: 2,
			},
			usage: TokenUsage{
				InputTokens:      new(uint64(1_000_000)),
				CacheReadTokens:  new(uint64(500_000)),
				CacheWriteTokens: new(uint64(250_000)),
			},
			wantCost: new(2.0),
		},
		{
			name: "cache rates fall back independently",
			model: models.Model{
				Input:     2,
				CacheRead: 0.5,
			},
			usage: TokenUsage{
				InputTokens:      new(uint64(1_000_000)),
				CacheReadTokens:  new(uint64(500_000)),
				CacheWriteTokens: new(uint64(250_000)),
			},
			wantCost: new(0.5 + 0.25 + 0.5),
		},
		{
			name:  "breakdown larger than input leaves no uncached tokens",
			model: priced,
			usage: TokenUsage{
				InputTokens:      new(uint64(100)),
				CacheReadTokens:  new(uint64(1_000_000)),
				CacheWriteTokens: new(uint64(1_000_000)),
			},
			wantCost: new(0.5 + 4),
		},
		{
			name:  "missing input still prices output",
			model: priced,
			usage: TokenUsage{
				OutputTokens: new(uint64(1_000_000)),
			},
			wantCost: new(10.0),
		},
		{
			name:  "reported zeroes cost nothing",
			model: priced,
			usage: TokenUsage{
				InputTokens:  new(uint64(0)),
				OutputTokens: new(uint64(0)),
			},
			wantCost: new(0.0),
		},
		{
			name: "model without prices stays unpriced",
			usage: TokenUsage{
				InputTokens: new(uint64(1_000_000)),
			},
		},
		{
			name:  "response without token counts stays unpriced",
			model: priced,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				actual := test.usage.Priced(test.model).Cost
				switch {
				case test.wantCost == nil:
					if actual != nil {
						t.Fatalf(
							"cost: %v",
							*actual,
						)
					}
				case actual == nil:
					t.Fatal("missing cost")
				case math.Abs(*actual-*test.wantCost) > 1e-9:
					t.Fatalf(
						"cost: got %v, want %v",
						*actual,
						*test.wantCost,
					)
				}
			},
		)
	}
}

func TestTokenUsageAddCost(t *testing.T) {
	for _, test := range []struct {
		previous  TokenUsage
		delta     TokenUsage
		wantCost  *float64
		name      string
		wantError bool
	}{
		{
			name: "first priced usage starts the total",
			delta: TokenUsage{
				Cost: new(1.5),
			},
			wantCost: new(1.5),
		},
		{
			name: "priced usage accumulates",
			previous: TokenUsage{
				Cost: new(1.5),
			},
			delta: TokenUsage{
				Cost: new(0.25),
			},
			wantCost: new(1.75),
		},
		{
			name: "unpriced usage keeps the total",
			previous: TokenUsage{
				Cost: new(1.5),
			},
			delta: TokenUsage{
				InputTokens: new(uint64(10)),
			},
			wantCost: new(1.5),
		},
		{
			name: "unpriced usage reports no total",
			delta: TokenUsage{
				InputTokens: new(uint64(10)),
			},
		},
		{
			name: "total that cannot be saved is rejected",
			previous: TokenUsage{
				Cost: new(math.MaxFloat64),
			},
			delta: TokenUsage{
				Cost: new(math.MaxFloat64),
			},
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				before, err := json.Marshal(test.previous)
				if err != nil {
					t.Fatal(err)
				}
				actual, addErr := test.previous.Add(test.delta)
				if (addErr != nil) != test.wantError {
					t.Fatalf(
						"cost error: %v",
						addErr,
					)
				}
				after, err := json.Marshal(test.previous)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("accumulation mutated an earlier checkpoint")
				}
				if addErr != nil {
					return
				}
				if test.wantCost == nil {
					if actual.Cost != nil {
						t.Fatalf(
							"cost: %v",
							*actual.Cost,
						)
					}
					return
				}
				if actual.Cost == nil || *actual.Cost != *test.wantCost {
					t.Fatalf(
						"cost: %#v",
						actual.Cost,
					)
				}
			},
		)
	}
}
