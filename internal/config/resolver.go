package config

import (
	"fmt"
	"time"

	"github.com/spachava753/gai"
)

// DefaultTimeout is the default request timeout when not specified in config or CLI
const DefaultTimeout = 5 * time.Minute

// ResolveConfig loads the config file and resolves effective runtime configuration
// for the specified model with runtime options applied.
func ResolveConfig(configPath string, opts RuntimeOptions) (*Config, error) {
	rawCfg, resolvedConfigPath, err := LoadRawConfigWithPath(configPath)
	if err != nil {
		return nil, err
	}

	return resolveFromRaw(rawCfg, opts, resolvedConfigPath)
}

// ResolveFromRaw resolves configuration from an already-loaded RawConfig.
// This is useful for testing without file I/O.
func ResolveFromRaw(rawCfg *RawConfig, opts RuntimeOptions) (*Config, error) {
	return resolveFromRaw(rawCfg, opts, "")
}

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

// resolveSystemPromptPath resolves system prompt path with precedence:
// model-specific > global defaults
func resolveSystemPromptPath(model ModelConfig, defaults Defaults) string {
	if model.SystemPromptPath != "" {
		return model.SystemPromptPath
	}
	return defaults.SystemPromptPath
}

// resolveGenerationParams merges generation parameters with precedence:
// CLI flags > Model-specific > Global defaults
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

// resolveTimeout resolves timeout with precedence:
// CLI flag > Global defaults > DefaultTimeout
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

// resolveCodeMode resolves code mode configuration with override behavior (not merge).
// Model-level completely replaces defaults.
func resolveCodeMode(model ModelConfig, defaults Defaults, configFilePath string) (*CodeModeConfig, error) {
	if model.CodeMode != nil {
		return normalizeCodeModeConfigPaths(model.CodeMode, configFilePath)
	}
	return normalizeCodeModeConfigPaths(defaults.CodeMode, configFilePath)
}
