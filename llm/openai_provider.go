package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements the LLMProvider interface for OpenAI API
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAIProvider creates a new OpenAIProvider with the given API key and optional configuration
func NewOpenAIProvider(apiKey string, baseURL string) *OpenAIProvider {
	config := openai.DefaultConfig(apiKey)

	if baseURL != "" {
		config.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(config)
	return &OpenAIProvider{
		client: client,
	}
}

// GenerateResponse generates a response using the OpenAI API
func (o *OpenAIProvider) GenerateResponse(config GenConfig, conversation Conversation) (Message, TokenUsage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	messages := convertToOpenAIMessages(conversation.SystemPrompt, conversation.Messages)

	tools := convertToOpenAITools(conversation.Tools)

	req := openai.ChatCompletionRequest{
		Model:       config.Model,
		Messages:    messages,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Stop:        config.Stop,
		Tools:       tools,
	}

	if config.TopP != nil {
		req.TopP = *config.TopP
	}
	if config.NumberOfResponses != nil {
		req.N = *config.NumberOfResponses
	}
	if config.PresencePenalty != nil {
		req.PresencePenalty = *config.PresencePenalty
	}
	if config.FrequencyPenalty != nil {
		req.FrequencyPenalty = *config.FrequencyPenalty
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
		return Message{}, TokenUsage{}, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return Message{}, TokenUsage{}, fmt.Errorf("no response generated")
	}

	tokenUsage := TokenUsage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
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
	}, tokenUsage, nil
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
