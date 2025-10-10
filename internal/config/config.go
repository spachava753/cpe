package config

import (
	"github.com/spachava753/cpe/internal/mcp"
)

// Model represents an AI model configuration
type Model struct {
	Name                 string              `json:"name" yaml:"name"`
	ID                   string              `json:"id" yaml:"id"`
	Type                 string              `json:"type" yaml:"type"`
	BaseUrl              string              `json:"base_url" yaml:"base_url"`
	ApiKeyEnv            string              `json:"api_key_env" yaml:"api_key_env"`
	ContextWindow        uint32              `json:"context_window" yaml:"context_window"`
	MaxOutput            uint32              `json:"max_output" yaml:"max_output"`
	InputCostPerMillion  float64             `json:"input_cost_per_million" yaml:"input_cost_per_million"`
	OutputCostPerMillion float64             `json:"output_cost_per_million" yaml:"output_cost_per_million"`
	PatchRequest         *PatchRequestConfig `json:"patchRequest,omitempty" yaml:"patchRequest,omitempty"`
}

// PatchRequestConfig holds configuration for patching HTTP requests
type PatchRequestConfig struct {
	JSONPatch      []map[string]interface{} `json:"jsonPatch,omitempty" yaml:"jsonPatch,omitempty"`
	IncludeHeaders map[string]string        `json:"includeHeaders,omitempty" yaml:"includeHeaders,omitempty"`
}

// Config represents the unified configuration structure
type Config struct {
	// MCP server configurations
	MCPServers map[string]mcp.ServerConfig `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`

	// Model definitions
	Models []ModelConfig `yaml:"models" json:"models"`

	// Default settings
	Defaults DefaultConfig `yaml:"defaults,omitempty" json:"defaults,omitempty"`

	// Version for future compatibility
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// ModelConfig extends the base model with generation defaults
type ModelConfig struct {
	Model `yaml:",inline" json:",inline"`

	// Optional override for the system prompt template path
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty"`

	// Generation parameter defaults for this model
	GenerationDefaults *GenerationParams `yaml:"generationDefaults,omitempty" json:"generationDefaults,omitempty"`
}

// GenerationParams holds generation parameters
type GenerationParams struct {
	Temperature       *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	TopP              *float64 `yaml:"topP,omitempty" json:"topP,omitempty"`
	TopK              *int     `yaml:"topK,omitempty" json:"topK,omitempty"`
	MaxTokens         *int     `yaml:"maxTokens,omitempty" json:"maxTokens,omitempty"`
	ThinkingBudget    *string  `yaml:"thinkingBudget,omitempty" json:"thinkingBudget,omitempty"`
	FrequencyPenalty  *float64 `yaml:"frequencyPenalty,omitempty" json:"frequencyPenalty,omitempty"`
	PresencePenalty   *float64 `yaml:"presencePenalty,omitempty" json:"presencePenalty,omitempty"`
	NumberOfResponses *int     `yaml:"numberOfResponses,omitempty" json:"numberOfResponses,omitempty"`
}

// DefaultConfig holds global defaults
type DefaultConfig struct {
	// Path to system prompt template file
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty"`

	// Default model to use if not specified
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	// Global generation parameter defaults
	GenerationParams *GenerationParams `yaml:"generationParams,omitempty" json:"generationParams,omitempty"`

	// Request timeout
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Disable streaming globally
	NoStream bool `yaml:"noStream,omitempty" json:"noStream,omitempty"`
}

// FindModel searches for a model by name in the config
func (c *Config) FindModel(name string) (*ModelConfig, bool) {
	for _, model := range c.Models {
		if model.Name == name {
			return &model, true
		}
	}
	return nil, false
}

// GetDefaultModel returns the default model name from config or environment
func (c *Config) GetDefaultModel() string {
	if c.Defaults.Model != "" {
		return c.Defaults.Model
	}
	return ""
}

// GetEffectiveSystemPromptPath resolves the system prompt path precedence.
// Priority: explicit CLI override > model override > global default.
func (m *ModelConfig) GetEffectiveSystemPromptPath(globalDefault, cliOverride string) string {
	if cliOverride != "" {
		return cliOverride
	}
	if m != nil && m.SystemPromptPath != "" {
		return m.SystemPromptPath
	}
	return globalDefault
}

// GetEffectiveGenerationParams returns the effective generation parameters by merging model defaults, global defaults, and CLI overrides.
func (m *ModelConfig) GetEffectiveGenerationParams(globalDefaults *GenerationParams, cliOverrides *GenerationParams) GenerationParams {
	result := GenerationParams{}

	if m != nil && m.GenerationDefaults != nil {
		result = *m.GenerationDefaults
	}

	if globalDefaults != nil {
		if result.Temperature == nil && globalDefaults.Temperature != nil {
			result.Temperature = globalDefaults.Temperature
		}
		if result.TopP == nil && globalDefaults.TopP != nil {
			result.TopP = globalDefaults.TopP
		}
		if result.TopK == nil && globalDefaults.TopK != nil {
			result.TopK = globalDefaults.TopK
		}
		if result.MaxTokens == nil && globalDefaults.MaxTokens != nil {
			result.MaxTokens = globalDefaults.MaxTokens
		}
		if result.ThinkingBudget == nil && globalDefaults.ThinkingBudget != nil {
			result.ThinkingBudget = globalDefaults.ThinkingBudget
		}
		if result.FrequencyPenalty == nil && globalDefaults.FrequencyPenalty != nil {
			result.FrequencyPenalty = globalDefaults.FrequencyPenalty
		}
		if result.PresencePenalty == nil && globalDefaults.PresencePenalty != nil {
			result.PresencePenalty = globalDefaults.PresencePenalty
		}
		if result.NumberOfResponses == nil && globalDefaults.NumberOfResponses != nil {
			result.NumberOfResponses = globalDefaults.NumberOfResponses
		}
	}

	if cliOverrides != nil {
		if cliOverrides.Temperature != nil {
			result.Temperature = cliOverrides.Temperature
		}
		if cliOverrides.TopP != nil {
			result.TopP = cliOverrides.TopP
		}
		if cliOverrides.TopK != nil {
			result.TopK = cliOverrides.TopK
		}
		if cliOverrides.MaxTokens != nil {
			result.MaxTokens = cliOverrides.MaxTokens
		}
		if cliOverrides.ThinkingBudget != nil {
			result.ThinkingBudget = cliOverrides.ThinkingBudget
		}
		if cliOverrides.FrequencyPenalty != nil {
			result.FrequencyPenalty = cliOverrides.FrequencyPenalty
		}
		if cliOverrides.PresencePenalty != nil {
			result.PresencePenalty = cliOverrides.PresencePenalty
		}
		if cliOverrides.NumberOfResponses != nil {
			result.NumberOfResponses = cliOverrides.NumberOfResponses
		}
	}

	return result
}
