package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
					InputCostPerMillion:      new(3.0),
					OutputCostPerMillion:     new(15.0),
					CacheReadCostPerMillion:  new(0.3),
					CacheWriteCostPerMillion: new(3.75),
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

func TestModelInfo_PrintsThinkingValues(t *testing.T) {
	t.Parallel()

	rawCfg := &config.RawConfig{Models: []config.ModelConfig{{
		Model: config.Model{
			Ref:           "reasoning-model",
			DisplayName:   "Reasoning Model",
			Type:          "responses",
			ID:            "gpt-5",
			ContextWindow: 400000,
			MaxOutput:     128000,
			ThinkingValues: []config.ThinkingValueConfig{
				{Value: "minimal", Name: "Minimal"},
				{Value: "xhigh", Name: "Extra High"},
				{Value: "10000"},
			},
		},
	}}}

	var out bytes.Buffer
	err := ModelInfo(context.Background(), ModelInfoOptions{Config: rawCfg, ModelName: "reasoning-model", Writer: &out})
	if err != nil {
		t.Fatalf("ModelInfo() error = %v", err)
	}

	want := "Ref: reasoning-model\n" +
		"Display Name: Reasoning Model\n" +
		"Type: responses\n" +
		"ID: gpt-5\n" +
		"Context: 400000\n" +
		"MaxOutput: 128000\n" +
		"InputCostPerMillion: n/a\n" +
		"OutputCostPerMillion: n/a\n" +
		"CacheReadCostPerMillion: n/a\n" +
		"CacheWriteCostPerMillion: n/a\n" +
		"Thinking Values:\n" +
		"  Minimal: minimal\n" +
		"  Extra High: xhigh\n" +
		"  10000: 10000\n"
	if got := out.String(); got != want {
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

func TestModelSystemPrompt_ResolvesPathRelativeToConfigFile(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "cpe.yaml")
	promptPath := filepath.Join(configDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Be helpful."), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	rawCfg := &config.RawConfig{
		Models: []config.ModelConfig{{
			Model: config.Model{
				Ref:           "test-model",
				DisplayName:   "Test Model",
				Type:          "openai",
				ID:            "gpt-4.1",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 128000,
				MaxOutput:     8192,
			},
			SystemPromptPath: "./prompt.md",
		}},
	}

	var out bytes.Buffer
	err := ModelSystemPrompt(context.Background(), ModelSystemPromptOptions{
		RawConfig:      rawCfg,
		ConfigFilePath: configPath,
		ModelName:      "test-model",
		Output:         &out,
	})
	if err != nil {
		t.Fatalf("ModelSystemPrompt() error = %v", err)
	}

	want := "Model: test-model\nPath: " + promptPath + "\n\nBe helpful.\n"
	if got := out.String(); got != want {
		t.Fatalf("ModelSystemPrompt() output mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
