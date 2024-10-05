package main

import (
	"fmt"
	"github.com/sashabaranov/go-openai"
	"os"

	"github.com/spachava753/cpe/llm"
)

type ModelDefaults struct {
	MaxTokens         int
	Temperature       float32
	TopP              float32
	TopK              int
	FrequencyPenalty  float32
	PresencePenalty   float32
	NumberOfResponses int
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

var modelConfigs = map[string]ModelConfig{
	"claude-3-opus": {
		Name: "claude-3-opus-20240229", ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 4096, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"claude-3-5-sonnet": {
		Name: "claude-3-5-sonnet-20240620", ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"claude-3-5-haiku": {
		Name: "claude-3-haiku-20240307", ProviderType: "anthropic", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 4096, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"gemini-1.5-flash-8b": {
		Name: "gemini-1.5-flash-8b", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"gemini-1.5-flash": {
		Name: "gemini-1.5-flash-002", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"gemini-1.5-pro": {
		Name: "gemini-1.5-pro-002", ProviderType: "gemini", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"gpt-4o": {
		Name: openai.GPT4o20240806, ProviderType: "openai", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
	"gpt-4o-mini": {
		Name: openai.GPT4oMini20240718, ProviderType: "openai", IsKnown: true,
		Defaults: ModelDefaults{MaxTokens: 8192, Temperature: 0.0, TopP: 1, TopK: 0, FrequencyPenalty: 0, PresencePenalty: 0, NumberOfResponses: 1},
	},
}

var defaultModel = "claude-3-5-sonnet"

func GetProvider(modelName string, flags Flags) (llm.LLMProvider, llm.GenConfig, error) {
	if modelName == "" {
		modelName = defaultModel
	}

	config, ok := modelConfigs[modelName]
	if !ok {
		// Handle unknown model
		if flags.CustomURL == "" {
			return nil, llm.GenConfig{}, fmt.Errorf("unknown model '%s' requires -custom-url flag", modelName)
		}
		fmt.Printf("Warning: Using unknown model '%s' with OpenAI provider\n", modelName)
		config = ModelConfig{Name: modelName, ProviderType: "openai", IsKnown: false}
	}

	genConfig := llm.GenConfig{
		Model:             config.Name,
		MaxTokens:         config.Defaults.MaxTokens,
		Temperature:       config.Defaults.Temperature,
		TopP:              config.Defaults.TopP,
		TopK:              config.Defaults.TopK,
		FrequencyPenalty:  config.Defaults.FrequencyPenalty,
		PresencePenalty:   config.Defaults.PresencePenalty,
		NumberOfResponses: config.Defaults.NumberOfResponses,
	}

	genConfig = flags.ApplyToGenConfig(genConfig)

	providerConfig, loadErr := loadProviderConfig(config.ProviderType)
	if loadErr != nil {
		return nil, llm.GenConfig{}, loadErr
	}

	var provider llm.LLMProvider
	var err error

	switch config.ProviderType {
	case "anthropic":
		provider = llm.NewAnthropicProvider(providerConfig.GetAPIKey(), flags.CustomURL)
	case "gemini":
		provider, err = llm.NewGeminiProvider(providerConfig.GetAPIKey(), flags.CustomURL)
	case "openai":
		provider = llm.NewOpenAIProvider(providerConfig.GetAPIKey(), flags.CustomURL)
	default:
		return nil, genConfig, fmt.Errorf("unsupported provider type: %s", config.ProviderType)
	}

	if err != nil {
		return nil, genConfig, err
	}

	return provider, genConfig, nil
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
