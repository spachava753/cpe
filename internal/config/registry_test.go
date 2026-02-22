package config

import (
	"strings"
	"testing"
)

func TestBuildModelFromRegistry_RequiresLimits(t *testing.T) {
	t.Parallel()

	provider := &ModelsDevProvider{ID: "openai", Name: "OpenAI", Env: []string{"OPENAI_API_KEY"}}
	model := &ModelsDevModel{ID: "gpt-5", Name: "GPT-5", Limit: nil}

	_, err := BuildModelFromRegistry(provider, model, "")
	if err == nil {
		t.Fatal("expected error when limits are missing")
	}
}

func TestBuildModelFromRegistry_SetsContextAndMaxOutput(t *testing.T) {
	t.Parallel()

	provider := &ModelsDevProvider{ID: "openai", Name: "OpenAI", Env: []string{"OPENAI_API_KEY"}, API: "https://api.openai.com/v1"}
	model := &ModelsDevModel{
		ID:   "gpt-5",
		Name: "GPT-5",
		Limit: &ModelsDevLimit{
			Context: 200000,
			Output:  64000,
		},
	}

	cfg, err := BuildModelFromRegistry(provider, model, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ContextWindow != 200000 {
		t.Fatalf("context window mismatch: got %d, want 200000", cfg.ContextWindow)
	}
	if cfg.MaxOutput != 64000 {
		t.Fatalf("max output mismatch: got %d, want 64000", cfg.MaxOutput)
	}
}

func TestBuildModelFromRegistry_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	provider := &ModelsDevProvider{ID: "unsupported-provider", Name: "Unsupported", Env: []string{"UNSUPPORTED_API_KEY"}}
	model := &ModelsDevModel{
		ID:   "test-model",
		Name: "Test Model",
		Limit: &ModelsDevLimit{
			Context: 8192,
			Output:  1024,
		},
	}

	_, err := BuildModelFromRegistry(provider, model, "")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "is not supported by CPE") {
		t.Fatalf("expected unsupported provider error, got: %v", err)
	}
}

func TestBuildModelFromRegistry_SetsPricingIncludingCacheCosts(t *testing.T) {
	t.Parallel()

	provider := &ModelsDevProvider{ID: "anthropic", Name: "Anthropic", Env: []string{"ANTHROPIC_API_KEY"}}
	model := &ModelsDevModel{
		ID:   "claude-sonnet-4-20250514",
		Name: "Claude Sonnet 4",
		Limit: &ModelsDevLimit{
			Context: 200000,
			Output:  64000,
		},
		Cost: &ModelsDevCost{
			Input:      float64Ptr(3),
			Output:     float64Ptr(15),
			CacheRead:  float64Ptr(0.3),
			CacheWrite: float64Ptr(3.75),
		},
	}

	cfg, err := BuildModelFromRegistry(provider, model, "sonnet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.InputCostPerMillion == nil || *cfg.InputCostPerMillion != 3 {
		t.Fatalf("input cost mismatch: got %v, want 3", cfg.InputCostPerMillion)
	}
	if cfg.OutputCostPerMillion == nil || *cfg.OutputCostPerMillion != 15 {
		t.Fatalf("output cost mismatch: got %v, want 15", cfg.OutputCostPerMillion)
	}
	if cfg.CacheReadCostPerMillion == nil || *cfg.CacheReadCostPerMillion != 0.3 {
		t.Fatalf("cache read cost mismatch: got %v, want 0.3", cfg.CacheReadCostPerMillion)
	}
	if cfg.CacheWriteCostPerMillion == nil || *cfg.CacheWriteCostPerMillion != 3.75 {
		t.Fatalf("cache write cost mismatch: got %v, want 3.75", cfg.CacheWriteCostPerMillion)
	}
}

func TestBuildModelFromRegistry_OmitsUnavailablePricingFields(t *testing.T) {
	t.Parallel()

	provider := &ModelsDevProvider{ID: "openai", Name: "OpenAI", Env: []string{"OPENAI_API_KEY"}}
	model := &ModelsDevModel{
		ID:   "gpt-5",
		Name: "GPT-5",
		Limit: &ModelsDevLimit{
			Context: 200000,
			Output:  64000,
		},
		Cost: &ModelsDevCost{
			Input:  float64Ptr(1.25),
			Output: float64Ptr(10),
		},
	}

	cfg, err := BuildModelFromRegistry(provider, model, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.InputCostPerMillion == nil || *cfg.InputCostPerMillion != 1.25 {
		t.Fatalf("input cost mismatch: got %v, want 1.25", cfg.InputCostPerMillion)
	}
	if cfg.OutputCostPerMillion == nil || *cfg.OutputCostPerMillion != 10 {
		t.Fatalf("output cost mismatch: got %v, want 10", cfg.OutputCostPerMillion)
	}
	if cfg.CacheReadCostPerMillion != nil {
		t.Fatalf("cache read cost should be nil, got %v", *cfg.CacheReadCostPerMillion)
	}
	if cfg.CacheWriteCostPerMillion != nil {
		t.Fatalf("cache write cost should be nil, got %v", *cfg.CacheWriteCostPerMillion)
	}
}

func TestBuildModelFromRegistry_OmitsAllPricingWhenCostMissing(t *testing.T) {
	t.Parallel()

	provider := &ModelsDevProvider{ID: "openai", Name: "OpenAI", Env: []string{"OPENAI_API_KEY"}}
	model := &ModelsDevModel{
		ID:   "gpt-5",
		Name: "GPT-5",
		Limit: &ModelsDevLimit{
			Context: 200000,
			Output:  64000,
		},
	}

	cfg, err := BuildModelFromRegistry(provider, model, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InputCostPerMillion != nil {
		t.Fatalf("input cost should be nil, got %v", *cfg.InputCostPerMillion)
	}
	if cfg.OutputCostPerMillion != nil {
		t.Fatalf("output cost should be nil, got %v", *cfg.OutputCostPerMillion)
	}
	if cfg.CacheReadCostPerMillion != nil {
		t.Fatalf("cache read cost should be nil, got %v", *cfg.CacheReadCostPerMillion)
	}
	if cfg.CacheWriteCostPerMillion != nil {
		t.Fatalf("cache write cost should be nil, got %v", *cfg.CacheWriteCostPerMillion)
	}
}

func float64Ptr(v float64) *float64 { return &v }
