package config

import (
	"text/template"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

// Model represents an AI model provider and capability configuration.
type Model struct {
	Ref                      string              `json:"ref" yaml:"ref" validate:"required" jsonschema:"required"`
	DisplayName              string              `json:"display_name" yaml:"display_name" validate:"required" jsonschema:"required"`
	ID                       string              `json:"id" yaml:"id" validate:"required" jsonschema:"required"`
	Type                     string              `json:"type" yaml:"type" validate:"required,oneof=openai anthropic gemini responses groq cerebras openrouter zai" jsonschema:"required"`
	BaseUrl                  string              `json:"base_url" yaml:"base_url,omitempty" validate:"omitempty,https_url|http_url"`
	ApiKeyEnv                string              `json:"api_key_env" yaml:"api_key_env" validate:"required_unless=AuthMethod oauth"`
	AuthMethod               string              `json:"auth_method" yaml:"auth_method,omitempty" validate:"omitempty,oneof=apikey oauth"`
	ContextWindow            uint32              `json:"context_window" yaml:"context_window,omitempty" validate:"required,gt=0" jsonschema:"required"`
	MaxOutput                uint32              `json:"max_output" yaml:"max_output,omitempty" validate:"required,gt=0" jsonschema:"required"`
	InputCostPerMillion      *float64            `json:"input_cost_per_million,omitempty" yaml:"input_cost_per_million,omitempty"`
	OutputCostPerMillion     *float64            `json:"output_cost_per_million,omitempty" yaml:"output_cost_per_million,omitempty"`
	CacheReadCostPerMillion  *float64            `json:"cache_read_cost_per_million,omitempty" yaml:"cache_read_cost_per_million,omitempty"`
	CacheWriteCostPerMillion *float64            `json:"cache_write_cost_per_million,omitempty" yaml:"cache_write_cost_per_million,omitempty"`
	PatchRequest             *PatchRequestConfig `json:"patchRequest,omitempty" yaml:"patchRequest,omitempty"`
}

// PatchRequestConfig holds configuration for patching HTTP requests.
type PatchRequestConfig struct {
	JSONPatch      []map[string]any  `json:"jsonPatch,omitempty" yaml:"jsonPatch,omitempty"`
	IncludeHeaders map[string]string `json:"includeHeaders,omitempty" yaml:"includeHeaders,omitempty"`
}

// CodeModeConfig controls code mode behavior for MCP tools.
type CodeModeConfig struct {
	Enabled              bool     `yaml:"enabled" json:"enabled"`
	ExcludedTools        []string `yaml:"excludedTools,omitempty" json:"excludedTools,omitempty"`
	LocalModulePaths     []string `yaml:"localModulePaths,omitempty" json:"localModulePaths,omitempty"`
	MaxTimeout           int      `yaml:"maxTimeout,omitempty" json:"maxTimeout,omitempty" validate:"omitempty,gte=0"`
	LargeOutputCharLimit int      `yaml:"largeOutputCharLimit,omitempty" json:"largeOutputCharLimit,omitempty" validate:"omitempty,gte=0"`
}

// RawCompactionConfig controls manual and threshold-driven conversation compaction.
type RawCompactionConfig struct {
	AutoTriggerThreshold      float64           `yaml:"autoTriggerThreshold,omitempty" json:"autoTriggerThreshold,omitempty" validate:"required,gt=0,max=1" jsonschema:"required"`
	MaxAutoCompactionRestarts int               `yaml:"maxAutoCompactionRestarts,omitempty" json:"maxAutoCompactionRestarts,omitempty" validate:"required,min=1" jsonschema:"required"`
	ToolDescription           string            `yaml:"toolDescription,omitempty" json:"toolDescription,omitempty" validate:"required" jsonschema:"required"`
	InputSchema               jsonschema.Schema `yaml:"inputSchema,omitempty" json:"inputSchema" jsonschema:"required,oneof_type=object;boolean" validate:"required"`
	InitialMessageTemplate    string            `yaml:"initialMessageTemplate,omitempty" json:"initialMessageTemplate,omitempty" validate:"required" jsonschema:"required"`
}

// RawConfig represents the YAML configuration file structure.
// NOTE: If you change schema-facing fields/tags in config structs, regenerate
// schema/cpe-config-schema.json with: go run ./build gen-schema
// (or go generate ./...).
type RawConfig struct {
	// Model profiles. Each entry is a complete runtime profile; there is no
	// defaults layer or field-level merging between models.
	Models []ModelConfig `yaml:"models" json:"models" validate:"gt=0,unique=Ref,dive" jsonschema:"required"`

	// Version for future compatibility.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// GenerationParams wraps gai.GenOpts with camelCase YAML tags for config unmarshaling.
// This adapter exists because gai.GenOpts uses snake_case tags matching API conventions.
type GenerationParams struct {
	Temperature         *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty" validate:"omitempty,lte=2,gte=0"`
	TopP                *float64 `yaml:"topP,omitempty" json:"topP,omitempty" validate:"omitempty,lte=1,gte=0"`
	TopK                *uint    `yaml:"topK,omitempty" json:"topK,omitempty" validate:"omitempty,gte=0"`
	FrequencyPenalty    *float64 `yaml:"frequencyPenalty,omitempty" json:"frequencyPenalty,omitempty" validate:"omitempty,lte=2,gte=-2"`
	PresencePenalty     *float64 `yaml:"presencePenalty,omitempty" json:"presencePenalty,omitempty" validate:"omitempty,lte=2,gte=-2"`
	N                   *uint    `yaml:"n,omitempty" json:"n,omitempty" validate:"omitempty,lte=2,gte=0"`
	MaxGenerationTokens *int     `yaml:"maxGenerationTokens,omitempty" json:"maxGenerationTokens,omitempty" validate:"omitempty,gte=0"`
	ToolChoice          string   `yaml:"toolChoice,omitempty" json:"toolChoice,omitempty"`
	StopSequences       []string `yaml:"stopSequences,omitempty" json:"stopSequences,omitempty"`
	ThinkingBudget      string   `yaml:"thinkingBudget,omitempty" json:"thinkingBudget,omitempty"`
}

// ToGenOpts converts GenerationParams to gai.GenOpts.
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

// ModelConfig is a complete runtime profile selected by --model or CPE_MODEL.
type ModelConfig struct {
	Model `yaml:",inline" json:",inline"`

	// MCP server configurations available when this model profile is selected.
	MCPServers map[string]mcpconfig.ServerConfig `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty" validate:"dive"`

	// Optional system prompt template path. Relative paths resolve from the config file directory.
	SystemPromptPath string `yaml:"systemPromptPath,omitempty" json:"systemPromptPath,omitempty" validate:"omitempty,filepath"`

	// Generation parameters for this model profile.
	GenerationParams *GenerationParams `yaml:"generationParams,omitempty" json:"generationParams,omitempty" validate:"omitempty"`

	// Request timeout for this model profile.
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Code mode configuration for this model profile.
	CodeMode *CodeModeConfig `yaml:"codeMode,omitempty" json:"codeMode,omitempty"`

	// Conversation compaction configuration for this model profile.
	Compaction *RawCompactionConfig `yaml:"compaction,omitempty" json:"compaction,omitempty" validate:"omitempty"`
}

// FindModel searches for a model profile by ref in the config.
func (c *RawConfig) FindModel(ref string) (ModelConfig, bool) {
	for _, model := range c.Models {
		if model.Ref == ref {
			return model, true
		}
	}
	return ModelConfig{}, false
}

const CompactionToolName = "compact_conversation"

// CompactionTemplateData is the data available to the compaction initial-message template.
type CompactionTemplateData struct {
	PreviousLeafID     string
	Dialog             gai.Dialog
	ToolArguments      map[string]any
	ToolArgumentsJSON  string
	CompactionToolName string
}

// CompactionConfig controls effective runtime conversation compaction behavior.
type CompactionConfig struct {
	TokenThreshold         uint
	MaxCompactions         uint
	Tool                   gai.Tool
	InitialMessageTemplate *template.Template
}

// Config represents the effective runtime configuration for one selected model profile.
type Config struct {
	// MCP server configurations for the selected model profile.
	MCPServers map[string]mcpconfig.ServerConfig

	// Selected model provider and capability settings.
	Model Model

	// Resolved system prompt path for the selected model profile.
	SystemPromptPath string

	// Effective generation parameters for the selected model profile and CLI overrides.
	GenerationParams *gai.GenOpts

	// Effective timeout.
	Timeout time.Duration

	// Effective code mode configuration.
	CodeMode *CodeModeConfig

	// Effective conversation compaction configuration.
	Compaction *CompactionConfig
}

// RuntimeOptions captures runtime overrides from CLI flags and environment.
type RuntimeOptions struct {
	// Model ref to use from --model or CPE_MODEL. Required.
	ModelRef string

	// Generation parameter overrides from flags.
	GenParams *gai.GenOpts

	// Timeout override from --timeout.
	Timeout string
}
