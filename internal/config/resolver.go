package config

import (
	"fmt"
	"time"

	"github.com/spachava753/gai"
)

// DefaultTimeout is the default request timeout when not specified in config or CLI
const DefaultTimeout = 5 * time.Minute

// ResolveConfig loads and resolves the effective runtime configuration for one model.
//
// Resolution contract:
//   - Config source: explicit configPath when provided, otherwise standard discovery.
//   - Model selection: opts.ModelRef > defaults.model (error when both are empty).
//   - System prompt path: model.systemPromptPath > defaults.systemPromptPath.
//   - Generation parameters: CLI/runtime opts > model.generation_defaults > defaults.generation_params.
//   - Timeout: CLI/runtime timeout > defaults.timeout > DefaultTimeout.
//   - Code mode: model.codeMode fully overrides defaults.codeMode (no field-level merge).
//   - Conversation storage path: defaults.conversationStoragePath, resolved relative to config file location when needed.
//
// The returned Config always has a non-nil GenerationDefaults pointer.
func ResolveConfig(configPath string, opts RuntimeOptions) (*Config, error) {
	rawCfg, resolvedConfigPath, err := LoadRawConfigWithPath(configPath)
	if err != nil {
		return nil, err
	}

	return resolveFromRaw(rawCfg, opts, resolvedConfigPath)
}

// ResolveFromRaw resolves configuration from an already-loaded RawConfig.
// It applies the same precedence rules as ResolveConfig but does not perform
// config file discovery or loading.
//
// Because no config file path is available, relative paths that depend on the
// config location (for example defaults.conversationStoragePath and codeMode
// localModulePaths) are resolved relative to the current process working
// directory via filepath.Abs semantics.
func ResolveFromRaw(rawCfg *RawConfig, opts RuntimeOptions) (*Config, error) {
	return resolveFromRaw(rawCfg, opts, "")
}

// resolveFromRaw orchestrates effective-config construction from a validated
// RawConfig and runtime overrides. It performs no I/O and returns deterministic
// output for the same inputs.
func resolveFromRaw(rawCfg *RawConfig, opts RuntimeOptions, resolvedConfigPath string) (*Config, error) {
	modelRef := opts.ModelRef
	if modelRef == "" {
		if rawCfg.Defaults.Model != "" {
			modelRef = rawCfg.Defaults.Model
		} else {
			return nil, fmt.Errorf("no model specified. Set CPE_MODEL environment variable, use --model flag, or set defaults.model in configuration")
		}
	}

	selectedModel, found := rawCfg.FindModel(modelRef)
	if !found {
		return nil, fmt.Errorf("model %q not found in configuration", modelRef)
	}

	systemPromptPath := resolveSystemPromptPath(selectedModel, rawCfg.Defaults)
	genParams, err := resolveGenerationParams(selectedModel, rawCfg.Defaults, opts)
	if err != nil {
		return nil, err
	}
	timeout, err := resolveTimeout(rawCfg.Defaults, opts)
	if err != nil {
		return nil, err
	}
	conversationStoragePath, err := ResolveConversationStoragePath(rawCfg.Defaults, resolvedConfigPath)
	if err != nil {
		return nil, fmt.Errorf("invalid defaults.conversationStoragePath: %w", err)
	}
	codeMode, err := resolveCodeMode(selectedModel, rawCfg.Defaults, resolvedConfigPath)
	if err != nil {
		return nil, fmt.Errorf("invalid codeMode configuration: %w", err)
	}

	return &Config{
		MCPServers:              rawCfg.MCPServers,
		Model:                   selectedModel.Model,
		SystemPromptPath:        systemPromptPath,
		GenerationDefaults:      genParams,
		Timeout:                 timeout,
		ConversationStoragePath: conversationStoragePath,
		CodeMode:                codeMode,
	}, nil
}

// resolveSystemPromptPath returns the effective system prompt path using
// model-level override first, then global defaults. It may return an empty
// string when neither source configures a prompt.
func resolveSystemPromptPath(model ModelConfig, defaults Defaults) string {
	if model.SystemPromptPath != "" {
		return model.SystemPromptPath
	}
	return defaults.SystemPromptPath
}

// resolveGenerationParams builds the effective generation options by layering
// defaults in precedence order: global defaults, then model defaults, then
// runtime/CLI overrides.
//
// The returned *gai.GenOpts is always non-nil.
func resolveGenerationParams(model ModelConfig, defaults Defaults, opts RuntimeOptions) (*gai.GenOpts, error) {
	genParams := &gai.GenOpts{}

	// Start with global defaults
	if defaults.GenerationParams != nil {
		mergeGenOpts(genParams, defaults.GenerationParams.ToGenOpts())
	}

	// Apply model-specific defaults
	if model.GenerationDefaults != nil {
		mergeGenOpts(genParams, model.GenerationDefaults.ToGenOpts())
	}

	// Apply CLI overrides
	if opts.GenParams != nil {
		mergeGenOpts(genParams, opts.GenParams)
	}

	return genParams, nil
}

// mergeGenOpts applies non-nil fields from src onto dst.
// A nil pointer in src means "not set" and leaves dst unchanged.
// A non-nil pointer in src (even pointing to zero) overrides dst.
// This is necessary to correctly distinguish "not set" (nil) from
// "explicitly set to zero" (non-nil pointer to zero value).
func mergeGenOpts(dst, src *gai.GenOpts) {
	if src == nil {
		return
	}
	if src.Temperature != nil {
		dst.Temperature = src.Temperature
	}
	if src.TopP != nil {
		dst.TopP = src.TopP
	}
	if src.TopK != nil {
		dst.TopK = src.TopK
	}
	if src.FrequencyPenalty != nil {
		dst.FrequencyPenalty = src.FrequencyPenalty
	}
	if src.PresencePenalty != nil {
		dst.PresencePenalty = src.PresencePenalty
	}
	if src.N != nil {
		dst.N = src.N
	}
	if src.MaxGenerationTokens != nil {
		dst.MaxGenerationTokens = src.MaxGenerationTokens
	}
	if src.ToolChoice != "" {
		dst.ToolChoice = src.ToolChoice
	}
	if src.StopSequences != nil {
		dst.StopSequences = src.StopSequences
	}
	if src.ThinkingBudget != "" {
		dst.ThinkingBudget = src.ThinkingBudget
	}
	if len(src.OutputModalities) > 0 {
		dst.OutputModalities = src.OutputModalities
	}
	if src.AudioConfig != (gai.AudioConfig{}) {
		dst.AudioConfig = src.AudioConfig
	}
	if src.ExtraArgs != nil {
		dst.ExtraArgs = src.ExtraArgs
	}
}

// resolveTimeout parses and resolves request timeout with precedence:
// runtime/CLI override > defaults.timeout > DefaultTimeout.
// Any invalid duration string fails resolution with a contextual error.
func resolveTimeout(defaults Defaults, opts RuntimeOptions) (time.Duration, error) {
	timeout := DefaultTimeout

	if opts.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(opts.Timeout)
		if err != nil {
			return 0, fmt.Errorf("invalid timeout value %q: %w", opts.Timeout, err)
		}
		timeout = parsedTimeout
	} else if defaults.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(defaults.Timeout)
		if err != nil {
			return 0, fmt.Errorf("invalid default timeout value %q: %w", defaults.Timeout, err)
		}
		timeout = parsedTimeout
	}

	return timeout, nil
}

// resolveCodeMode resolves and normalizes effective code mode config.
//
// Contract: model.codeMode, when present, replaces defaults.codeMode entirely.
// There is no field-level merge between model and global scopes.
func resolveCodeMode(model ModelConfig, defaults Defaults, configFilePath string) (*CodeModeConfig, error) {
	if model.CodeMode != nil {
		return normalizeCodeModeConfigPaths(model.CodeMode, configFilePath)
	}
	return normalizeCodeModeConfigPaths(defaults.CodeMode, configFilePath)
}
