package config

import (
	"fmt"
	"path/filepath"
	"text/template"
	"time"

	"github.com/spachava753/gai"
)

// DefaultTimeout is the request timeout when neither a model profile nor runtime override sets one.
const DefaultTimeout = 5 * time.Minute

// ResolveConfig loads and resolves the effective runtime configuration for one model profile.
//
// Resolution contract:
//   - Config source: explicit configPath when provided, otherwise standard discovery.
//   - Model selection: opts.ModelRef is required; callers populate it from ACP
//     session state, --model, or CPE_MODEL depending on entrypoint.
//   - Model profile fields are resolved from the selected profile as written.
//   - Runtime generation and timeout overrides take precedence over the selected
//     model profile.
//
// The returned Config always has a non-nil GenerationParams pointer.
func ResolveConfig(configPath string, opts RuntimeOptions) (Config, error) {
	rawCfg, resolvedConfigPath, err := LoadRawConfigWithPath(configPath)
	if err != nil {
		return Config{}, err
	}

	return resolveFromRaw(rawCfg, opts, resolvedConfigPath)
}

// ResolveFromRaw resolves configuration from an already-loaded RawConfig.
// It applies the same rules as ResolveConfig but does not perform config file discovery or loading.
func ResolveFromRaw(rawCfg *RawConfig, opts RuntimeOptions) (Config, error) {
	return resolveFromRaw(rawCfg, opts, "")
}

// resolveFromRaw constructs the effective runtime config for the selected model profile.
func resolveFromRaw(rawCfg *RawConfig, opts RuntimeOptions, resolvedConfigPath string) (Config, error) {
	if opts.ModelRef == "" {
		return Config{}, fmt.Errorf("no model specified. Set CPE_MODEL or pass --model")
	}

	selectedModel, found := rawCfg.FindModel(opts.ModelRef)
	if !found {
		return Config{}, fmt.Errorf("model %q not found in configuration", opts.ModelRef)
	}
	if err := validateSelectedProfile(selectedModel); err != nil {
		return Config{}, fmt.Errorf("invalid selected model profile %q: %w", opts.ModelRef, err)
	}

	systemPromptPath := resolveSystemPromptPath(selectedModel, resolvedConfigPath)
	genParams := resolveGenerationParams(selectedModel, opts)
	timeout, err := resolveTimeout(selectedModel, opts)
	if err != nil {
		return Config{}, err
	}
	compaction, err := resolveCompaction(selectedModel)
	if err != nil {
		return Config{}, fmt.Errorf("invalid compaction configuration: %w", err)
	}

	return Config{
		MCPServers:       selectedModel.MCPServers,
		Model:            selectedModel.Model,
		SystemPromptPath: systemPromptPath,
		GenerationParams: genParams,
		Timeout:          timeout,
		CodeMode:         selectedModel.CodeMode,
		DisableEditTool:  selectedModel.DisableEditTool,
		Compaction:       compaction,
	}, nil
}

// resolveSystemPromptPath returns the model profile's prompt path, resolving relative paths from the config file directory when available.
func resolveSystemPromptPath(model ModelConfig, configFilePath string) string {
	path := model.SystemPromptPath
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) || configFilePath == "" {
		return path
	}
	return filepath.Join(filepath.Dir(configFilePath), path)
}

// resolveGenerationParams returns the model profile's generation parameters with runtime overrides applied.
func resolveGenerationParams(model ModelConfig, opts RuntimeOptions) *gai.GenOpts {
	genParams := &gai.GenOpts{}
	if model.GenerationParams != nil {
		MergeGenOpts(genParams, model.GenerationParams.ToGenOpts())
	}
	if opts.GenParams != nil {
		MergeGenOpts(genParams, opts.GenParams)
	}
	return genParams
}

// MergeGenOpts applies non-zero presence fields from src onto dst.
// Pointer fields use nil to mean "not set"; a non-nil pointer to a zero value still overrides.
// A nil src is a no-op.
func MergeGenOpts(dst, src *gai.GenOpts) {
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

// resolveTimeout parses the timeout with precedence: runtime override > model profile timeout > DefaultTimeout.
func resolveTimeout(model ModelConfig, opts RuntimeOptions) (time.Duration, error) {
	rawTimeout := model.Timeout
	if opts.Timeout != "" {
		rawTimeout = opts.Timeout
	}
	if rawTimeout == "" {
		return DefaultTimeout, nil
	}
	parsedTimeout, err := time.ParseDuration(rawTimeout)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout value %q: %w", rawTimeout, err)
	}
	return parsedTimeout, nil
}

func resolveCompaction(model ModelConfig) (*CompactionConfig, error) {
	raw := model.Compaction
	if raw == nil {
		return nil, nil
	}

	tmpl, err := template.New("compaction_initial_message").Parse(raw.InitialMessageTemplate)
	if err != nil {
		return nil, fmt.Errorf("initialMessageTemplate: %w", err)
	}

	return &CompactionConfig{
		TokenThreshold: uint(float64(model.ContextWindow) * raw.AutoTriggerThreshold),
		MaxCompactions: uint(raw.MaxAutoCompactionRestarts),
		Tool: gai.Tool{
			Name:        CompactionToolName,
			Description: raw.ToolDescription,
			InputSchema: &raw.InputSchema,
		},
		InitialMessageTemplate: tmpl,
	}, nil
}
