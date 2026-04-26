package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
)

func ptr[T any](v T) *T { return &v }

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
		Temperature:         ptr(0.7),
		MaxGenerationTokens: ptr(1024),
		StopSequences:       []string{"model-stop"},
	}
	result := resolveGenerationParams(model, RuntimeOptions{GenParams: &gai.GenOpts{
		Temperature:   ptr(0.2),
		StopSequences: []string{"cli-stop"},
	}})

	checkPtr(t, "Temperature", result.Temperature, ptr(0.2))
	checkPtr(t, "MaxGenerationTokens", result.MaxGenerationTokens, ptr(1024))
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

func TestResolveCodeMode_ResolvesRelativePathsAgainstConfigFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := makeGoModule(t, root, "helpers")

	model := testModelProfile()
	model.CodeMode = &CodeModeConfig{Enabled: true, LocalModulePaths: []string{"./helpers"}}
	rawCfg := &RawConfig{Version: "1.0", Models: []ModelConfig{model}}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{ModelRef: "test-model"}, filepath.Join(root, "cpe.yaml"))
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.CodeMode == nil || len(cfg.CodeMode.LocalModulePaths) != 1 {
		t.Fatalf("expected one local module path, got %#v", cfg.CodeMode)
	}
	if got, want := cfg.CodeMode.LocalModulePaths[0], canonicalPath(moduleDir); got != want {
		t.Fatalf("unexpected local module path: got %q want %q", got, want)
	}
}

func TestResolveCodeMode_DuplicateLocalModulePathsError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := makeGoModule(t, root, "helpers")
	model := testModelProfile()
	model.CodeMode = &CodeModeConfig{Enabled: true, LocalModulePaths: []string{"./helpers", moduleDir}}

	_, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{model}}, RuntimeOptions{ModelRef: "test-model"}, filepath.Join(root, "cpe.yaml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "invalid selected model profile \"test-model\": codeMode: localModulePaths contains duplicate path: " + canonicalPath(moduleDir)
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestResolveIgnoresUnselectedProfileRuntimeValidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	goodModuleDir := makeGoModule(t, root, "helpers")
	selected := testModelProfile()
	selected.CodeMode = &CodeModeConfig{Enabled: true, LocalModulePaths: []string{"./helpers"}}

	unselected := testModelProfile()
	unselected.Ref = "broken-profile"
	unselected.CodeMode = &CodeModeConfig{Enabled: true, LocalModulePaths: []string{"./missing"}}

	cfg, err := resolveFromRaw(&RawConfig{Models: []ModelConfig{selected, unselected}}, RuntimeOptions{ModelRef: "test-model"}, filepath.Join(root, "cpe.yaml"))
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.CodeMode == nil || len(cfg.CodeMode.LocalModulePaths) != 1 {
		t.Fatalf("expected selected codeMode path, got %#v", cfg.CodeMode)
	}
	if got, want := cfg.CodeMode.LocalModulePaths[0], canonicalPath(goodModuleDir); got != want {
		t.Fatalf("selected localModulePath = %q, want %q", got, want)
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

func makeGoModule(t *testing.T, root, name string) string {
	t.Helper()
	moduleDir := filepath.Join(root, name)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("creating module directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/"+name+"\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("writing module go.mod: %v", err)
	}
	return moduleDir
}

func canonicalPath(path string) string {
	cleaned := filepath.Clean(path)
	if realPath, err := filepath.EvalSymlinks(cleaned); err == nil {
		return filepath.Clean(realPath)
	}
	return cleaned
}
