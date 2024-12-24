package llm

import (
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"log/slog"
	"os"
)

type ModelDefaults struct {
	MaxTokens         int
	Temperature       float32
	TopP              *float32
	TopK              *int
	FrequencyPenalty  *float32
	PresencePenalty   *float32
	NumberOfResponses *int
}

type ModelConfig struct {
	Name         string
	ProviderType string
	IsKnown      bool
	Defaults     ModelDefaults
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
	"claude-3-opus": {
		Name: anthropic.ModelClaude_3_Opus_20240229, ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 4096, Temperature: 0.3},
	},
	"claude-3-5-sonnet": {
		Name: anthropic.ModelClaude3_5Sonnet20241022, ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"claude-3-5-haiku": {
		Name: anthropic.ModelClaude3_5Haiku20241022, ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"claude-3-haiku": {
		Name: anthropic.ModelClaude_3_Haiku_20240307, ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 4096, Temperature: 0.3},
	},
	"gemini-1.5-flash-8b": {
		Name: "gemini-1.5-flash-8b", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"gemini-1.5-flash": {
		Name: "gemini-1.5-flash-002", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"gemini-2.0-flash-exp": {
		Name: "gemini-2.0-flash-exp", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"gemini-1.5-pro": {
		Name: "gemini-1.5-pro-002", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"gpt-4o": {
		Name: openai.ChatModelGPT4o2024_11_20, ProviderType: "openai", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"gpt-4o-mini": {
		Name: openai.ChatModelGPT4oMini2024_07_18, ProviderType: "openai", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.3},
	},
	"o1": {
		Name: openai.ChatModelO1_2024_12_17, ProviderType: "openai", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 100000, Temperature: 1},
	},
}

var DefaultModel = "claude-3-5-sonnet"

type ModelOptions struct {
	Model             string
	CustomURL         string
	MaxTokens         int
	Temperature       float64
	TopP              float64
	TopK              int
	FrequencyPenalty  float64
	PresencePenalty   float64
	NumberOfResponses int
	Input             string
	Version           bool
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
	return config
}

func GetProvider(logger *slog.Logger, modelName string, flags ModelOptions) (LLMProvider, GenConfig, error) {
	if modelName == "" {
		modelName = DefaultModel
	}

	config, ok := ModelConfigs[modelName]
	if !ok {
		// Handle unknown model
		if flags.CustomURL == "" {
			return nil, GenConfig{}, fmt.Errorf("unknown model '%s' requires -custom-url flag or CPE_CUSTOM_URL environment variable", modelName)
		}
		logger.Info("Using unknown model with OpenAI provider", slog.String("model", modelName), slog.String("custom-url", flags.CustomURL))
		config = ModelConfig{Name: modelName, ProviderType: "openai", IsKnown: false}
	}

	genConfig := GenConfig{
		Model:       config.Name,
		MaxTokens:   config.Defaults.MaxTokens,
		Temperature: config.Defaults.Temperature,
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

	providerConfig, loadErr := loadProviderConfig(config.ProviderType)
	if loadErr != nil {
		return nil, GenConfig{}, loadErr
	}

	var provider LLMProvider
	var err error

	switch config.ProviderType {
	case "anthropic":
		provider = NewAnthropicProvider(providerConfig.GetAPIKey(), flags.CustomURL)
	case "gemini":
		provider, err = NewGeminiProvider(providerConfig.GetAPIKey(), flags.CustomURL)
	case "openai":
		provider = NewOpenAIProvider(providerConfig.GetAPIKey(), flags.CustomURL)
	default:
		return nil, genConfig, fmt.Errorf("unsupported provider type: %s", config.ProviderType)
	}

	return provider, genConfig, err
}

func loadProviderConfig(providerType string) (ProviderConfig, error) {
	switch providerType {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
		}
		return AnthropicConfig{APIKey: apiKey}, nil
	case "gemini":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
		}
		return GeminiConfig{APIKey: apiKey}, nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
		}
		return OpenAIConfig{APIKey: apiKey}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}
