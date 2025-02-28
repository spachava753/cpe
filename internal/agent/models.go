package agent

import (
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
)

// GenConfig represents the configuration when invoking a model.
// This helps divorce what model is invoked vs. what provider is used,
// so the same provider can invoke different models.
type GenConfig struct {
	Model             string
	MaxTokens         int
	Temperature       float32  // Controls randomness: 0.0 - 1.0
	TopP              *float32 // Controls diversity: 0.0 - 1.0
	TopK              *int     // Controls token sampling:
	FrequencyPenalty  *float32 // Penalizes frequent tokens: -2.0 - 2.0
	PresencePenalty   *float32 // Penalizes repeated tokens: -2.0 - 2.0
	Stop              []string // List of sequences where the API will stop generating further tokens
	NumberOfResponses *int     // Number of chat completion choices to generate
	ToolChoice        string   // Controls tool use: "auto", "any", or "tool"
	ForcedTool        string   // Name of the tool to force when ToolChoice is "tool"
	ThinkingBudget    string   // Budget for reasoning/thinking capabilities
	ReasoningEffort   string   // OpenAI SDK reasoning effort parameter
}

type ModelDefaults struct {
	MaxTokens         int
	Temperature       float32
	TopP              *float32
	TopK              *int
	FrequencyPenalty  *float32
	PresencePenalty   *float32
	NumberOfResponses *int
	ThinkingBudget    string  // Default budget for reasoning/thinking capabilities
}

type ModelConfig struct {
	Name            string
	IsKnown         bool
	Defaults        ModelDefaults
	SupportedInputs []InputType
}

type ProviderConfig interface {
	GetAPIKey() string
}

type AnthropicConfig struct {
	APIKey string
}

func (c AnthropicConfig) GetAPIKey() string {
	return c.APIKey
}

type GeminiConfig struct {
	APIKey string
}

func (c GeminiConfig) GetAPIKey() string {
	return c.APIKey
}

type OpenAIConfig struct {
	APIKey string
}

func (c OpenAIConfig) GetAPIKey() string {
	return c.APIKey
}

var ModelConfigs = map[string]ModelConfig{
	"o3-mini": {
		Name: openai.ChatModelO3Mini, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 100000, Temperature: 1, ThinkingBudget: "low"},
		SupportedInputs: []InputType{InputTypeText},
	},
	"deepseek-chat": {
		Name: "deepseek-chat", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText},
	},
	"deepseek-r1": {
		Name: "deepseek-reasoner", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 1},
		SupportedInputs: []InputType{InputTypeText},
	},
	"claude-3-7-sonnet": {
		Name: anthropic.ModelClaude3_7Sonnet20250219, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 64000, Temperature: 0.3, ThinkingBudget: "0"},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"claude-3-opus": {
		Name: anthropic.ModelClaude_3_Opus_20240229, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 4096, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"claude-3-5-sonnet": {
		Name: anthropic.ModelClaude3_5Sonnet20241022, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"claude-3-5-haiku": {
		Name: anthropic.ModelClaude3_5Haiku20241022, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"claude-3-haiku": {
		Name: anthropic.ModelClaude_3_Haiku_20240307, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 4096, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"gemini-1-5-flash-8b": {
		Name: "gemini-1.5-flash-8b", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gemini-1-5-flash": {
		Name: "gemini-1.5-flash-002", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gemini-2-flash-exp": {
		Name: "gemini-2.0-flash-exp", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gemini-2-flash": {
		Name: "gemini-2.0-flash", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gemini-2-flash-lite-preview": {
		Name: "gemini-2.0-flash-lite-preview-02-05", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gemini-2-pro-exp": {
		Name: "gemini-2.0-pro-exp-02-05", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gemini-1-5-pro": {
		Name: "gemini-1.5-pro-002", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage, InputTypeVideo, InputTypeAudio},
	},
	"gpt-4o": {
		Name: openai.ChatModelGPT4o2024_11_20, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"gpt-4o-mini": {
		Name: openai.ChatModelGPT4oMini2024_07_18, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
		SupportedInputs: []InputType{InputTypeText, InputTypeImage},
	},
	"o1": {
		Name: openai.ChatModelO1_2024_12_17, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 100000, Temperature: 1, ThinkingBudget: "medium"},
		SupportedInputs: []InputType{InputTypeText},
	},
	"o1-mini": {
		Name: openai.ChatModelO1Mini2024_09_12, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 65536, Temperature: 1, ThinkingBudget: "low"},
		SupportedInputs: []InputType{InputTypeText},
	},
	"o1-preview": {
		Name: openai.ChatModelO1Preview2024_09_12, IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 100000, Temperature: 1, ThinkingBudget: "medium"},
		SupportedInputs: []InputType{InputTypeText},
	},
}

var DefaultModel = "claude-3-7-sonnet"

type ModelOptions struct {
	Model              string
	CustomURL          string
	MaxTokens          int
	Temperature        float64
	TopP               float64
	TopK               int
	FrequencyPenalty   float64
	PresencePenalty    float64
	NumberOfResponses  int
	ThinkingBudget     string // Budget for reasoning/thinking capabilities of models that support it
	Version            bool
	Continue           string // conversation ID to continue from
	ListConversations  bool   // List all conversations
	DeleteConversation string // Conversation ID to delete
	DeleteCascade      bool   // Delete conversation and all children
	PrintConversation  string // Conversation ID to print
	New                bool   // Start a new conversation instead of continuing from the last one
}

func (f ModelOptions) ApplyToGenConfig(config GenConfig) GenConfig {
	if f.MaxTokens != 0 {
		config.MaxTokens = f.MaxTokens
	}
	if f.Temperature != 0 {
		config.Temperature = float32(f.Temperature)
	}
	if f.TopP != 0 {
		topP := float32(f.TopP)
		config.TopP = &topP
	}
	if f.TopK != 0 {
		topK := f.TopK
		config.TopK = &topK
	}
	if f.FrequencyPenalty != 0 {
		freqPenalty := float32(f.FrequencyPenalty)
		config.FrequencyPenalty = &freqPenalty
	}
	if f.PresencePenalty != 0 {
		presPenalty := float32(f.PresencePenalty)
		config.PresencePenalty = &presPenalty
	}
	if f.NumberOfResponses != 0 {
		numResponses := f.NumberOfResponses
		config.NumberOfResponses = &numResponses
	}
	if f.ThinkingBudget != "" {
		config.ThinkingBudget = f.ThinkingBudget
	}
	return config
}

func GetConfig(flags ModelOptions) (GenConfig, error) {
	config, ok := ModelConfigs[flags.Model]
	if !ok {
		// Handle unknown model
		if flags.CustomURL == "" {
			return GenConfig{}, fmt.Errorf("unknown model '%s' requires -custom-url flag or CPE_CUSTOM_URL environment variable", flags.Model)
		}
		config = ModelConfig{Name: flags.Model, IsKnown: false}
	}

	genConfig := GenConfig{
		Model:          config.Name,
		MaxTokens:      config.Defaults.MaxTokens,
		Temperature:    config.Defaults.Temperature,
		ThinkingBudget: config.Defaults.ThinkingBudget,
	}

	if config.Defaults.TopP != nil {
		genConfig.TopP = config.Defaults.TopP
	}
	if config.Defaults.TopK != nil {
		genConfig.TopK = config.Defaults.TopK
	}
	if config.Defaults.FrequencyPenalty != nil {
		genConfig.FrequencyPenalty = config.Defaults.FrequencyPenalty
	}
	if config.Defaults.PresencePenalty != nil {
		genConfig.PresencePenalty = config.Defaults.PresencePenalty
	}
	if config.Defaults.NumberOfResponses != nil {
		genConfig.NumberOfResponses = config.Defaults.NumberOfResponses
	}

	genConfig = flags.ApplyToGenConfig(genConfig)

	return genConfig, nil
}
