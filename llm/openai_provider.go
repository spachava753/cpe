package llm

import (
	"context"
	"fmt"
	"github.com/spachava753/cpe/validate"
	"time"

	"github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements the LLMProvider interface for OpenAI API
type OpenAIProvider struct {
	client *openai.Client
}

// OpenAIOption is a functional option for configuring the OpenAIProvider
type OpenAIOption func(*openai.ClientConfig)

// WithBaseURL sets a custom base URL for the OpenAI API
func WithBaseURL(url string) OpenAIOption {
	return func(c *openai.ClientConfig) {
		if url != "" {
			if _, err := validate.ValidateURL(url); err != nil {
				fmt.Printf("Warning: Invalid OpenAI base URL provided. Using default.\n")
				return
			}
			c.BaseURL = url
		}
	}
}

// NewOpenAIProvider creates a new OpenAIProvider with the given API key and optional configuration
func NewOpenAIProvider(apiKey string, opts ...OpenAIOption) *OpenAIProvider {
	config := openai.DefaultConfig(apiKey)

	for _, opt := range opts {
		opt(&config)
	}

	client := openai.NewClientWithConfig(config)
	return &OpenAIProvider{
		client: client,
	}
}

// GenerateResponse generates a response using the OpenAI API
func (o *OpenAIProvider) GenerateResponse(config ModelConfig, conversation Conversation) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	messages := convertToOpenAIMessages(conversation.SystemPrompt, conversation.Messages)

	resp, err := o.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:            config.Model,
			Messages:         messages,
			Temperature:      config.Temperature,
			MaxTokens:        config.MaxTokens,
			TopP:             config.TopP,
			N:                config.NumberOfResponses,
			PresencePenalty:  config.PresencePenalty,
			FrequencyPenalty: config.FrequencyPenalty,
			Stop:             config.Stop,
		},
	)

	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response generated")
	}

	return resp.Choices[0].Message.Content, nil
}

// convertToOpenAIMessages converts internal Message type to OpenAI's ChatCompletionMessage
func convertToOpenAIMessages(systemPrompt string, messages []Message) []openai.ChatCompletionMessage {
	openAIMessages := make([]openai.ChatCompletionMessage, len(messages)+1)
	openAIMessages[0] = openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}
	for i, msg := range messages {
		openAIMessages[i+1] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}
	return openAIMessages
}
