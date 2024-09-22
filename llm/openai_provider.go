package llm

import (
	"context"
	"encoding/json"
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
func (o *OpenAIProvider) GenerateResponse(config GenConfig, conversation Conversation) (Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	messages := convertToOpenAIMessages(conversation.SystemPrompt, conversation.Messages)

	tools := convertToOpenAITools(conversation.Tools)

	req := openai.ChatCompletionRequest{
		Model:            config.Model,
		Messages:         messages,
		Temperature:      config.Temperature,
		MaxTokens:        config.MaxTokens,
		TopP:             config.TopP,
		N:                config.NumberOfResponses,
		PresencePenalty:  config.PresencePenalty,
		FrequencyPenalty: config.FrequencyPenalty,
		Stop:             config.Stop,
		Tools:            tools,
	}

	if config.ToolChoice != "" {
		req.ToolChoice = config.ToolChoice
		if config.ForcedTool != "" {
			req.ToolChoice = &openai.ToolChoice{
				Type: openai.ToolTypeFunction,
				Function: openai.ToolFunction{
					Name: config.ForcedTool,
				},
			}
		}
	}

	resp, err := o.client.CreateChatCompletion(ctx, req)

	if err != nil {
		return Message{}, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return Message{}, fmt.Errorf("no response generated")
	}

	var contentBlocks []ContentBlock
	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: choice.Message.Content})
		}
		if choice.Message.ToolCalls != nil {
			for _, toolCall := range choice.Message.ToolCalls {
				contentBlocks = append(contentBlocks, ContentBlock{
					Type: "tool_use",
					ToolUse: &ToolUse{
						ID:    toolCall.ID,
						Name:  toolCall.Function.Name,
						Input: json.RawMessage(toolCall.Function.Arguments),
					},
				})
			}
		}
	}
	return Message{
		Role:    "assistant",
		Content: contentBlocks,
	}, nil
}

// convertToOpenAIMessages converts internal Message type to OpenAI's ChatCompletionMessage
func convertToOpenAIMessages(systemPrompt string, messages []Message) []openai.ChatCompletionMessage {
	openAIMessages := make([]openai.ChatCompletionMessage, len(messages)+1)
	openAIMessages[0] = openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}
	for i, msg := range messages {
		openAIMessage := openai.ChatCompletionMessage{
			Role: msg.Role,
		}

		for _, content := range msg.Content {
			switch content.Type {
			case "text":
				openAIMessage.Content = content.Text
			case "tool_use":
				openAIMessage.ToolCalls = append(openAIMessage.ToolCalls, openai.ToolCall{
					ID: content.ToolUse.ID,
					Function: openai.FunctionCall{
						Name:      content.ToolUse.Name,
						Arguments: string(content.ToolUse.Input),
					},
				})
			case "tool_result":
				openAIMessage.ToolCallID = content.ToolResult.ToolUseID
				openAIMessage.Content = fmt.Sprintf("%v", content.ToolResult.Content)
			}
		}

		openAIMessages[i+1] = openAIMessage
	}
	return openAIMessages
}

// convertToOpenAITools converts internal Tool type to OpenAI's Tool
func convertToOpenAITools(tools []Tool) []openai.Tool {
	openAITools := make([]openai.Tool, len(tools))
	for i, tool := range tools {
		openAITools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		}
	}
	return openAITools
}
