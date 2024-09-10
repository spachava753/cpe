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
	IsKnown      bool
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
	"claude-3-opus":     {Name: "claude-3-opus-20240229", ProviderType: "anthropic", IsKnown: true},
	"claude-3-5-sonnet": {Name: "claude-3-5-sonnet-20240620", ProviderType: "anthropic", IsKnown: true},
	"claude-3-5-haiku":  {Name: "claude-3-haiku-20240307", ProviderType: "anthropic", IsKnown: true},
	"gemini-1.5-flash":  {Name: "gemini-1.5-flash", ProviderType: "gemini", IsKnown: true},
	"gpt-4o":            {Name: openai.GPT4o20240806, ProviderType: "openai", IsKnown: true},
	"gpt-4o-mini":       {Name: openai.GPT4oMini20240718, ProviderType: "openai", IsKnown: true},
	// Add more models here
}

var defaultModel = "claude-3-5-sonnet"

func GetProvider(modelName, openaiURL string) (llm.LLMProvider, ModelConfig, error) {
	if modelName == "" {
		modelName = defaultModel
	}

	config, ok := modelConfigs[modelName]
	if !ok {
		// Handle unknown model
		if openaiURL == "" {
			return nil, ModelConfig{}, fmt.Errorf("unknown model '%s' requires -openai-url flag", modelName)
		}
		fmt.Printf("Warning: Using unknown model '%s' with OpenAI provider\n", modelName)
		config = ModelConfig{Name: modelName, ProviderType: "openai", IsKnown: false}
	}

	providerConfig, err := loadProviderConfig(config.ProviderType)
	if err != nil {
		return nil, ModelConfig{}, err
	}

	switch config.ProviderType {
	case "anthropic":
		return llm.NewAnthropicProvider(providerConfig.GetAPIKey()), config, nil
	case "gemini":
		p, gemErr := llm.NewGeminiProvider(providerConfig.GetAPIKey())
		return p, config, gemErr
	case "openai":
		return llm.NewOpenAIProvider(providerConfig.GetAPIKey(), llm.WithBaseURL(openaiURL)), config, nil
	default:
		return nil, config, fmt.Errorf("unsupported provider type: %s", config.ProviderType)
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
