package conversation

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/spachava753/gai"
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
