package config

import "testing"

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
