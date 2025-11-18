package config

//go:generate go run github.com/spachava753/cpe/cmd/gen-schema

import (
	"time"

	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/gai"
)

// Model represents an AI model configuration
type Model struct {
	Ref                  string              `json:"ref" yaml:"ref" validate:"required"`
	DisplayName          string              `json:"display_name" yaml:"display_name" validate:"required"`
	ID                   string              `json:"id" yaml:"id" validate:"required"`
	Type                 string              `json:"type" yaml:"type" validate:"required,oneof=openai anthropic gemini responses groq cerebras openrouter"`
	BaseUrl              string              `json:"base_url" yaml:"base_url" validate:"omitempty,https_url|http_url"`
	ApiKeyEnv            string              `json:"api_key_env" yaml:"api_key_env" validate:"required"`
	ContextWindow        uint32              `json:"context_window" yaml:"context_window" validate:"omitempty,gt=0"`
	MaxOutput            uint32              `json:"max_output" yaml:"max_output" validate:"omitempty,gt=0"`
	InputCostPerMillion  float64             `json:"input_cost_per_million" yaml:"input_cost_per_million"`
	OutputCostPerMillion float64             `json:"output_cost_per_million" yaml:"output_cost_per_million"`
	PatchRequest         *PatchRequestConfig `json:"patchRequest,omitempty" yaml:"patchRequest,omitempty"`
}

// PatchRequestConfig holds configuration for patching HTTP requests
type PatchRequestConfig struct {
	JSONPatch      []map[string]interface{} `json:"jsonPatch,omitempty" yaml:"jsonPatch,omitempty"`
	IncludeHeaders map[string]string        `json:"includeHeaders,omitempty" yaml:"includeHeaders,omitempty"`
}

// RawConfig represents the configuration file structure
type RawConfig struct {
	// MCP server configurations
	MCPServers map[string]mcp.ServerConfig `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty" validate:"dive"`

	// Model definitions
	Models []ModelConfig `yaml:"models" json:"models" validate:"gt=0,unique=Ref,dive"`

	// Default settings
	Defaults Defaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`

	// Version for future compatibility
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type Defaults struct {
	// Path to system prompt template file
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty" validate:"omitempty,filepath"`

	// Default model to use if not specified
	Model string `yaml:"model,omitempty" json:"model,omitempty" validate:"omitempty"`

	// Global generation parameter defaults
	GenerationParams *gai.GenOpts `yaml:"generationParams,omitempty" json:"generationParams,omitempty" validate:"omitempty"`

	// Request timeout
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Disable streaming globally
	NoStream bool `yaml:"noStream,omitempty" json:"noStream,omitempty"`
}

// ModelConfig extends the base model with generation defaults
type ModelConfig struct {
	Model `yaml:",inline" json:",inline"`

	// Optional override for the system prompt template path
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty" validate:"omitempty,filepath"`

	// Generation parameter defaults for this model
	GenerationDefaults *gai.GenOpts `yaml:"generationDefaults,omitempty" json:"generationDefaults,omitempty" validate:"omitempty"`
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

	// Whether streaming is disabled
	NoStream bool
}

// RuntimeOptions captures runtime overrides from CLI flags and environment
type RuntimeOptions struct {
	// Model ref to use (from --model or CPE_MODEL)
	ModelRef string

	// Generation parameter overrides (from flags)
	GenParams *gai.GenOpts

	// Timeout override (from --timeout)
	Timeout string

	// Streaming override (from --no-stream)
	NoStream *bool
}
