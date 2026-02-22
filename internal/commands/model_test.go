package commands

import (
	"bytes"
	"context"
	"testing"

	"github.com/spachava753/cpe/internal/config"
)

func TestModelInfo_PrintsCachePricingFields(t *testing.T) {
	t.Parallel()

	rawCfg := &config.RawConfig{
		Models: []config.ModelConfig{
			{
				Model: config.Model{
					Ref:                      "test-model",
					DisplayName:              "Test Model",
					Type:                     "openai",
					ID:                       "gpt-4.1",
					ContextWindow:            128000,
					MaxOutput:                8192,
					InputCostPerMillion:      float64Ptr(3),
					OutputCostPerMillion:     float64Ptr(15),
					CacheReadCostPerMillion:  float64Ptr(0.3),
					CacheWriteCostPerMillion: float64Ptr(3.75),
				},
			},
		},
	}

	var out bytes.Buffer
	err := ModelInfo(context.Background(), ModelInfoOptions{
		Config:    rawCfg,
		ModelName: "test-model",
		Writer:    &out,
	})
	if err != nil {
		t.Fatalf("ModelInfo() error = %v", err)
	}

	want := "Ref: test-model\n" +
		"Display Name: Test Model\n" +
		"Type: openai\n" +
		"ID: gpt-4.1\n" +
		"Context: 128000\n" +
		"MaxOutput: 8192\n" +
		"InputCostPerMillion: 3.000000\n" +
		"OutputCostPerMillion: 15.000000\n" +
		"CacheReadCostPerMillion: 0.300000\n" +
		"CacheWriteCostPerMillion: 3.750000\n"

	got := out.String()
	if got != want {
		t.Fatalf("ModelInfo() output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestModelInfo_PrintsNAWhenPricingUnavailable(t *testing.T) {
	t.Parallel()

	rawCfg := &config.RawConfig{
		Models: []config.ModelConfig{
			{
				Model: config.Model{
					Ref:           "free-model",
					DisplayName:   "Free Model",
					Type:          "responses",
					ID:            "gpt-free",
					ContextWindow: 64000,
					MaxOutput:     4096,
				},
			},
		},
	}

	var out bytes.Buffer
	err := ModelInfo(context.Background(), ModelInfoOptions{
		Config:    rawCfg,
		ModelName: "free-model",
		Writer:    &out,
	})
	if err != nil {
		t.Fatalf("ModelInfo() error = %v", err)
	}

	want := "Ref: free-model\n" +
		"Display Name: Free Model\n" +
		"Type: responses\n" +
		"ID: gpt-free\n" +
		"Context: 64000\n" +
		"MaxOutput: 4096\n" +
		"InputCostPerMillion: n/a\n" +
		"OutputCostPerMillion: n/a\n" +
		"CacheReadCostPerMillion: n/a\n" +
		"CacheWriteCostPerMillion: n/a\n"

	got := out.String()
	if got != want {
		t.Fatalf("ModelInfo() output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func float64Ptr(v float64) *float64 { return &v }
