package main

import (
	"fmt"
	"github.com/sashabaranov/go-openai"
	"os"

	"github.com/spachava753/cpe/llm"
)

type ModelConfig struct {
	Name         string
	ProviderType string
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
	"claude-3-opus":     {Name: "claude-3-opus-20240229", ProviderType: "anthropic"},
	"claude-3-5-sonnet": {Name: "claude-3-5-sonnet-20240620", ProviderType: "anthropic"},
	"claude-3-5-haiku":  {Name: "claude-3-haiku-20240307", ProviderType: "anthropic"},
	"gemini-1.5-flash":  {Name: "gemini-1.5-flash", ProviderType: "gemini"},
	"gpt-4o":            {Name: openai.GPT4o20240806, ProviderType: "openai"},
	"gpt-4o-mini":       {Name: openai.GPT4oMini20240718, ProviderType: "openai"},
	// Add more models here
}

var defaultModel = "claude-3-5-sonnet-20240620"

func GetProvider(modelName string) (llm.LLMProvider, error) {
	if modelName == "" {
		modelName = defaultModel
	}

	config, ok := modelConfigs[modelName]
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", modelName)
	}

	providerConfig, err := loadProviderConfig(config.ProviderType)
	if err != nil {
		return nil, err
	}

	switch config.ProviderType {
	case "anthropic":
		return llm.NewAnthropicProvider(providerConfig.GetAPIKey()), nil
	case "gemini":
		return llm.NewGeminiProvider(providerConfig.GetAPIKey())
	case "openai":
		return llm.NewOpenAIProvider(providerConfig.GetAPIKey()), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", config.ProviderType)
	}
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
