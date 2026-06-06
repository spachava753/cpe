package config

import (
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
)

func TestResolveConfigRequiresModel(t *testing.T) {
	_, err := ResolveFromRaw(&RawConfig{Models: []ModelConfig{testModelProfile()}}, RuntimeOptions{})
	if err == nil {
		t.Fatal("expected missing model error")
	}
	want := "no model specified. Set CPE_MODEL or pass --model"
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestResolveGenerationParamsUsesModelProfileAndCLIOnly(t *testing.T) {
	model := testModelProfile()
	model.GenerationParams = &GenerationParams{
		Temperature:         new(0.7),
		MaxGenerationTokens: new(1024),
		StopSequences:       []string{"model-stop"},
	}
	result := resolveGenerationParams(model, RuntimeOptions{GenParams: &gai.GenOpts{
		Temperature:   new(0.2),
		StopSequences: []string{"cli-stop"},
	}})

	checkPtr(t, "Temperature", result.Temperature, new(0.2))
	checkPtr(t, "MaxGenerationTokens", result.MaxGenerationTokens, new(1024))
	if got, want := result.StopSequences, []string{"cli-stop"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("StopSequences = %v, want %v", got, want)
	}
}

func TestResolveTimeoutUsesModelProfileAndCLI(t *testing.T) {
	model := testModelProfile()
	model.Timeout = "30s"
	got, err := resolveTimeout(model, RuntimeOptions{})
	if err != nil {
		t.Fatalf("resolveTimeout returned error: %v", err)
	}
	if got != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", got)
	}

	got, err = resolveTimeout(model, RuntimeOptions{Timeout: "2m"})
	if err != nil {
		t.Fatalf("resolveTimeout override returned error: %v", err)
	}
	if got != 2*time.Minute {
		t.Fatalf("timeout = %s, want 2m", got)
	}
}

func TestResolveDisableEditTool(t *testing.T) {
	t.Parallel()

	defaultModel := testModelProfile()
	disabledModel := testModelProfile()
	disabledModel.Ref = "without-edit"
	disabledModel.DisableEditTool = true

	cfg, err := ResolveFromRaw(&RawConfig{Models: []ModelConfig{defaultModel, disabledModel}}, RuntimeOptions{ModelRef: "test-model"})
	if err != nil {
		t.Fatalf("ResolveFromRaw default returned error: %v", err)
	}
	if cfg.DisableEditTool {
		t.Fatal("DisableEditTool default = true, want false")
	}

	cfg, err = ResolveFromRaw(&RawConfig{Models: []ModelConfig{defaultModel, disabledModel}}, RuntimeOptions{ModelRef: "without-edit"})
	if err != nil {
		t.Fatalf("ResolveFromRaw disabled returned error: %v", err)
	}
	if !cfg.DisableEditTool {
		t.Fatal("DisableEditTool = false, want true")
	}
}

func TestResolveCompactionFromModelProfile(t *testing.T) {
	t.Parallel()

	model := testModelProfile()
	model.ContextWindow = 1000
	model.Compaction = &RawCompactionConfig{
		AutoTriggerThreshold:      0.25,
		MaxAutoCompactionRestarts: 2,
		ToolDescription:           "model compact",
		InputSchema:               jsonschema.Schema{Type: "object"},
		InitialMessageTemplate:    "model {{ .ToolArgumentsJSON }}",
	}

	cfg, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: "test-model"}, "")
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.Compaction == nil {
		t.Fatal("expected compaction config")
	}
	if cfg.Compaction.TokenThreshold != 250 {
		t.Fatalf("TokenThreshold = %d, want 250", cfg.Compaction.TokenThreshold)
	}
	if cfg.Compaction.MaxCompactions != 2 {
		t.Fatalf("MaxCompactions = %d, want 2", cfg.Compaction.MaxCompactions)
	}
	if cfg.Compaction.Tool.Description != "model compact" {
		t.Fatalf("tool description = %q", cfg.Compaction.Tool.Description)
	}
}

func TestResolveCompactionInvalidTemplateFails(t *testing.T) {
	t.Parallel()

	model := testModelProfile()
	model.Compaction = &RawCompactionConfig{
		AutoTriggerThreshold: 0.5, MaxAutoCompactionRestarts: 1, ToolDescription: "compact", InputSchema: jsonschema.Schema{Type: "object"}, InitialMessageTemplate: "{{",
	}

	_, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: "test-model"}, "")
	if err == nil {
		t.Fatal("expected invalid template error")
	}
}

func checkPtr[T comparable](t *testing.T, name string, got, want *T) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s: expected nil, got %v", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s: expected %v, got nil", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s: expected %v, got %v", name, *want, *got)
	}
}

func testModelProfile() ModelConfig {
	return ModelConfig{Model: Model{
		Ref:           "test-model",
		DisplayName:   "Test Model",
		ID:            "test-id",
		Type:          "openai",
		ApiKeyEnv:     "OPENAI_API_KEY",
		ContextWindow: 200000,
		MaxOutput:     64000,
	}}
}
