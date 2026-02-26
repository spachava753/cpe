package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spachava753/gai"
)

func ptr[T any](v T) *T { return &v }

func TestResolveGenerationParams(t *testing.T) {
	tests := []struct {
		name               string
		globalDefaults     *GenerationParams
		modelDefaults      *GenerationParams
		cliOverrides       *gai.GenOpts
		wantTemp           *float64
		wantMaxTokens      *int
		wantTopP           *float64
		wantStopSequences  []string
		wantThinkingBudget string
	}{
		{
			name:           "global defaults only",
			globalDefaults: &GenerationParams{Temperature: ptr(0.7), MaxGenerationTokens: ptr(1024)},
			wantTemp:       ptr(0.7),
			wantMaxTokens:  ptr(1024),
		},
		{
			name:           "model overrides global",
			globalDefaults: &GenerationParams{Temperature: ptr(0.7), MaxGenerationTokens: ptr(1024)},
			modelDefaults:  &GenerationParams{Temperature: ptr(0.3)},
			wantTemp:       ptr(0.3),
			wantMaxTokens:  ptr(1024), // inherited from global
		},
		{
			name:           "CLI overrides model",
			globalDefaults: &GenerationParams{Temperature: ptr(0.9)},
			modelDefaults:  &GenerationParams{Temperature: ptr(0.7)},
			cliOverrides:   &gai.GenOpts{Temperature: ptr(0.5)},
			wantTemp:       ptr(0.5),
		},
		{
			name:          "CLI sets temperature to zero overrides non-zero",
			modelDefaults: &GenerationParams{Temperature: ptr(0.7)},
			cliOverrides:  &gai.GenOpts{Temperature: ptr(0.0)},
			wantTemp:      ptr(0.0),
		},
		{
			name:          "CLI nil does not override model default",
			modelDefaults: &GenerationParams{Temperature: ptr(0.7)},
			cliOverrides:  &gai.GenOpts{Temperature: nil, MaxGenerationTokens: ptr(2048)},
			wantTemp:      ptr(0.7),
			wantMaxTokens: ptr(2048),
		},
		{
			name:          "all nil sources",
			wantTemp:      nil,
			wantMaxTokens: nil,
		},
		{
			name:           "model sets field to zero overrides global",
			globalDefaults: &GenerationParams{TopP: ptr(0.9)},
			modelDefaults:  &GenerationParams{TopP: ptr(0.0)},
			wantTopP:       ptr(0.0),
		},
		{
			name:              "StopSequences CLI overrides model",
			modelDefaults:     &GenerationParams{StopSequences: []string{"stop1"}},
			cliOverrides:      &gai.GenOpts{StopSequences: []string{"stop2", "stop3"}},
			wantStopSequences: []string{"stop2", "stop3"},
		},
		{
			name:              "StopSequences nil CLI preserves model default",
			modelDefaults:     &GenerationParams{StopSequences: []string{"stop1"}},
			cliOverrides:      &gai.GenOpts{},
			wantStopSequences: []string{"stop1"},
		},
		{
			name:               "ThinkingBudget model overrides global",
			globalDefaults:     &GenerationParams{ThinkingBudget: "low"},
			modelDefaults:      &GenerationParams{ThinkingBudget: "high"},
			wantThinkingBudget: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaults := Defaults{GenerationParams: tt.globalDefaults}
			model := ModelConfig{GenerationDefaults: tt.modelDefaults}
			opts := RuntimeOptions{GenParams: tt.cliOverrides}

			result, err := resolveGenerationParams(model, defaults, opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			checkPtr(t, "Temperature", result.Temperature, tt.wantTemp)
			checkPtr(t, "MaxGenerationTokens", result.MaxGenerationTokens, tt.wantMaxTokens)
			checkPtr(t, "TopP", result.TopP, tt.wantTopP)

			if tt.wantStopSequences != nil {
				if len(result.StopSequences) != len(tt.wantStopSequences) {
					t.Errorf("StopSequences: expected %v, got %v", tt.wantStopSequences, result.StopSequences)
				} else {
					for i, want := range tt.wantStopSequences {
						if result.StopSequences[i] != want {
							t.Errorf("StopSequences[%d]: expected %q, got %q", i, want, result.StopSequences[i])
						}
					}
				}
			}

			if tt.wantThinkingBudget != "" && result.ThinkingBudget != tt.wantThinkingBudget {
				t.Errorf("ThinkingBudget: expected %q, got %q", tt.wantThinkingBudget, result.ThinkingBudget)
			}
		})
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

func TestResolveCodeMode_ResolvesRelativePathsAgainstConfigFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := filepath.Join(root, "helpers")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("creating module directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/helpers\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("writing module go.mod: %v", err)
	}

	cfgPath := filepath.Join(root, "cpe.yaml")
	rawCfg := &RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test",
				ID:            "gpt-4o-mini",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 128000,
				MaxOutput:     16384,
			},
		}},
		Defaults: Defaults{
			Model: "test-model",
			CodeMode: &CodeModeConfig{
				Enabled:          true,
				LocalModulePaths: []string{"./helpers"},
			},
		},
	}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{}, cfgPath)
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}

	if cfg.CodeMode == nil {
		t.Fatal("expected code mode config, got nil")
	}
	if len(cfg.CodeMode.LocalModulePaths) != 1 {
		t.Fatalf("expected 1 local module path, got %d", len(cfg.CodeMode.LocalModulePaths))
	}

	wantModulePath := canonicalPath(moduleDir)
	if cfg.CodeMode.LocalModulePaths[0] != wantModulePath {
		t.Fatalf("unexpected local module path: got %q want %q", cfg.CodeMode.LocalModulePaths[0], wantModulePath)
	}
}

func TestResolveCodeMode_DuplicateLocalModulePathsError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := filepath.Join(root, "helpers")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("creating module directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/helpers\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("writing module go.mod: %v", err)
	}

	rawCfg := &RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test",
				ID:            "gpt-4o-mini",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 128000,
				MaxOutput:     16384,
			},
		}},
		Defaults: Defaults{
			Model: "test-model",
			CodeMode: &CodeModeConfig{
				Enabled:          true,
				LocalModulePaths: []string{"./helpers", moduleDir},
			},
		},
	}

	_, err := resolveFromRaw(rawCfg, RuntimeOptions{}, filepath.Join(root, "cpe.yaml"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	want := "invalid codeMode configuration: localModulePaths contains duplicate path: " + canonicalPath(moduleDir)
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestResolveCodeMode_ModelCodeModeOverridesDefaults(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	defaultsModuleDir := filepath.Join(root, "defaults-module")
	modelModuleDir := filepath.Join(root, "model-module")
	for _, moduleDir := range []string{defaultsModuleDir, modelModuleDir} {
		if err := os.MkdirAll(moduleDir, 0o755); err != nil {
			t.Fatalf("creating module directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/"+filepath.Base(moduleDir)+"\n\ngo 1.24\n"), 0o644); err != nil {
			t.Fatalf("writing module go.mod: %v", err)
		}
	}

	cfgPath := filepath.Join(root, "cpe.yaml")
	rawCfg := &RawConfig{
		Version: "1.0",
		Models: []ModelConfig{{
			Model: Model{
				Ref:           "test-model",
				DisplayName:   "Test",
				ID:            "gpt-4o-mini",
				Type:          "openai",
				ApiKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 128000,
				MaxOutput:     16384,
			},
			CodeMode: &CodeModeConfig{
				Enabled:          true,
				LocalModulePaths: []string{"./model-module"},
			},
		}},
		Defaults: Defaults{
			Model: "test-model",
			CodeMode: &CodeModeConfig{
				Enabled:          true,
				LocalModulePaths: []string{"./defaults-module"},
			},
		},
	}

	cfg, err := resolveFromRaw(rawCfg, RuntimeOptions{}, cfgPath)
	if err != nil {
		t.Fatalf("resolveFromRaw returned error: %v", err)
	}
	if cfg.CodeMode == nil {
		t.Fatal("expected code mode config, got nil")
	}
	if len(cfg.CodeMode.LocalModulePaths) != 1 {
		t.Fatalf("expected exactly 1 localModulePath, got %d", len(cfg.CodeMode.LocalModulePaths))
	}

	want := canonicalPath(modelModuleDir)
	if cfg.CodeMode.LocalModulePaths[0] != want {
		t.Fatalf("unexpected local module path: got %q want %q", cfg.CodeMode.LocalModulePaths[0], want)
	}
}

func canonicalPath(path string) string {
	cleaned := filepath.Clean(path)
	if realPath, err := filepath.EvalSymlinks(cleaned); err == nil {
		return filepath.Clean(realPath)
	}
	return cleaned
}
