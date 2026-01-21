package config

import (
	"time"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcp"
)

// Model represents an AI model configuration
type Model struct {
	Ref                  string              `json:"ref" yaml:"ref" validate:"required"`
	DisplayName          string              `json:"display_name" yaml:"display_name" validate:"required"`
	ID                   string              `json:"id" yaml:"id" validate:"required"`
	Type                 string              `json:"type" yaml:"type" validate:"required,oneof=openai anthropic gemini responses groq cerebras openrouter zai"`
	BaseUrl              string              `json:"base_url" yaml:"base_url" validate:"omitempty,https_url|http_url"`
	ApiKeyEnv            string              `json:"api_key_env" yaml:"api_key_env" validate:"required_unless=AuthMethod oauth"`
	AuthMethod           string              `json:"auth_method" yaml:"auth_method" validate:"omitempty,oneof=apikey oauth"`
	ContextWindow        uint32              `json:"context_window" yaml:"context_window" validate:"omitempty,gt=0"`
	MaxOutput            uint32              `json:"max_output" yaml:"max_output" validate:"omitempty,gt=0"`
	InputCostPerMillion  float64             `json:"input_cost_per_million" yaml:"input_cost_per_million"`
	OutputCostPerMillion float64             `json:"output_cost_per_million" yaml:"output_cost_per_million"`
	PatchRequest         *PatchRequestConfig `json:"patchRequest,omitempty" yaml:"patchRequest,omitempty"`
}

// PatchRequestConfig holds configuration for patching HTTP requests
type PatchRequestConfig struct {
	JSONPatch      []map[string]any  `json:"jsonPatch,omitempty" yaml:"jsonPatch,omitempty"`
	IncludeHeaders map[string]string `json:"includeHeaders,omitempty" yaml:"includeHeaders,omitempty"`
}

// CodeModeConfig controls code mode behavior for MCP tools
type CodeModeConfig struct {
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	ExcludedTools []string `yaml:"excludedTools,omitempty" json:"excludedTools,omitempty"`
	MaxTimeout    int      `yaml:"maxTimeout,omitempty" json:"maxTimeout,omitempty" validate:"omitempty,gte=0"`
}

// SubagentConfig defines a subagent for MCP server mode
type SubagentConfig struct {
	// Name is the tool name exposed via MCP (required)
	Name string `yaml:"name" json:"name" validate:"required"`
	// Description is the tool description exposed via MCP (required)
	Description string `yaml:"description" json:"description" validate:"required"`
	// OutputSchemaPath is an optional path to a JSON schema file for structured output
	OutputSchemaPath string `yaml:"outputSchemaPath,omitempty" json:"outputSchemaPath,omitempty"`
}

// RawConfig represents the configuration file structure
type RawConfig struct {
	// MCP server configurations
	MCPServers map[string]mcp.ServerConfig `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty" validate:"dive"`

	// Model definitions
	Models []ModelConfig `yaml:"models" json:"models" validate:"gt=0,unique=Ref,dive"`

	// Default settings
	Defaults Defaults `yaml:"defaults,omitempty" json:"defaults"`

	// Subagent configuration for MCP server mode
	Subagent *SubagentConfig `yaml:"subagent,omitempty" json:"subagent,omitempty" validate:"omitempty"`

	// Version for future compatibility
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// GenerationParams wraps gai.GenOpts with proper YAML/JSON tags for config unmarshaling
type GenerationParams struct {
	Temperature         float64  `yaml:"temperature,omitempty" json:"temperature,omitempty" validate:"omitempty,lte=2,gte=0"`
	TopP                float64  `yaml:"topP,omitempty" json:"topP,omitempty" validate:"omitempty,lte=1,gte=0"`
	TopK                uint     `yaml:"topK,omitempty" json:"topK,omitempty" validate:"omitempty,gte=0"`
	FrequencyPenalty    float64  `yaml:"frequencyPenalty,omitempty" json:"frequencyPenalty,omitempty" validate:"omitempty,lte=2,gte=-2"`
	PresencePenalty     float64  `yaml:"presencePenalty,omitempty" json:"presencePenalty,omitempty" validate:"omitempty,lte=2,gte=-2"`
	N                   uint     `yaml:"n,omitempty" json:"n,omitempty" validate:"omitempty,lte=2,gte=0"`
	MaxGenerationTokens int      `yaml:"maxGenerationTokens,omitempty" json:"maxGenerationTokens,omitempty" validate:"omitempty,gte=0"`
	ToolChoice          string   `yaml:"toolChoice,omitempty" json:"toolChoice,omitempty"`
	StopSequences       []string `yaml:"stopSequences,omitempty" json:"stopSequences,omitempty"`
	ThinkingBudget      string   `yaml:"thinkingBudget,omitempty" json:"thinkingBudget,omitempty" validate:"omitempty,oneof=minimal low medium high|number"`
}

// ToGenOpts converts GenerationParams to gai.GenOpts
func (g *GenerationParams) ToGenOpts() *gai.GenOpts {
	if g == nil {
		return nil
	}
	return &gai.GenOpts{
		Temperature:         g.Temperature,
		TopP:                g.TopP,
		TopK:                g.TopK,
		FrequencyPenalty:    g.FrequencyPenalty,
		PresencePenalty:     g.PresencePenalty,
		N:                   g.N,
		MaxGenerationTokens: g.MaxGenerationTokens,
		ToolChoice:          g.ToolChoice,
		StopSequences:       g.StopSequences,
		ThinkingBudget:      g.ThinkingBudget,
	}
}

type Defaults struct {
	// Path to system prompt template file
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty" validate:"omitempty,filepath"`

	// Default model to use if not specified
	Model string `yaml:"model,omitempty" json:"model,omitempty" validate:"omitempty"`

	// Global generation parameter defaults
	GenerationParams *GenerationParams `yaml:"generationParams,omitempty" json:"generationParams,omitempty" validate:"omitempty"`

	// Request timeout
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`


	// Code mode configuration
	CodeMode *CodeModeConfig `yaml:"codeMode,omitempty" json:"codeMode,omitempty"`
}

// ModelConfig extends the base model with generation defaults
type ModelConfig struct {
	Model `yaml:",inline" json:",inline"`

	// Optional override for the system prompt template path
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty" validate:"omitempty,filepath"`

	// Generation parameter defaults for this model
	GenerationDefaults *GenerationParams `yaml:"generationDefaults,omitempty" json:"generationDefaults,omitempty" validate:"omitempty"`

	// Code mode configuration for this model (overrides global defaults)
	CodeMode *CodeModeConfig `yaml:"codeMode,omitempty" json:"codeMode,omitempty"`
}

// FindModel searches for a model by ref in the config
func (c *RawConfig) FindModel(ref string) (ModelConfig, bool) {
	for _, model := range c.Models {
		if model.Ref == ref {
			return model, true
		}
	}
	return ModelConfig{}, false
}

// Config represents the effective runtime configuration for a single model
type Config struct {
	// MCP server configurations
	MCPServers map[string]mcp.ServerConfig

	// Selected model
	Model Model

	// Resolved system prompt path (model-specific or global default)
	SystemPromptPath string

	// Effective generation parameters after merging all sources
	GenerationDefaults *gai.GenOpts

	// Effective timeout
	Timeout time.Duration


	// Effective code mode configuration
	CodeMode *CodeModeConfig
}

// RuntimeOptions captures runtime overrides from CLI flags and environment
type RuntimeOptions struct {
	// Model ref to use (from --model or CPE_MODEL)
	ModelRef string

	// Generation parameter overrides (from flags)
	GenParams *gai.GenOpts

	// Timeout override (from --timeout)
	Timeout string

}
