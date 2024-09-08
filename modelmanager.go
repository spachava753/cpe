package main

import (
	"fmt"
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

var modelConfigs = map[string]ModelConfig{
	"claude-3-5-sonnet-20240620": {Name: "claude-3-5-sonnet-20240620", ProviderType: "anthropic"},
	"gemini-1.5-flash":           {Name: "gemini-1.5-flash", ProviderType: "gemini"},
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
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}
