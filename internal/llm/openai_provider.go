package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAIProvider implements the LLMProvider interface for OpenAI API
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAIProvider creates a new OpenAIProvider with the given API key and optional configuration
func NewOpenAIProvider(apiKey string, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(5),
		option.WithRequestTimeout(5 * time.Minute),
	}
	if baseURL != "" {
		// Ensure baseURL ends with a trailing "/"
		if !strings.HasSuffix(baseURL, "/") {
			baseURL = baseURL + "/"
		}
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(opts...)
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

	params := openai.ChatCompletionNewParams{
		Model:       openai.F(config.Model),
		Messages:    openai.F(messages),
		Temperature: openai.Float(float64(config.Temperature)),
		MaxTokens:   openai.Int(int64(config.MaxTokens)),
		Stop:        openai.F[openai.ChatCompletionNewParamsStopUnion](openai.ChatCompletionNewParamsStopArray(config.Stop)),
		Tools:       openai.F(tools),
	}

	if config.TopP != nil {
		params.TopP = openai.Float(float64(*config.TopP))
	}
	if config.NumberOfResponses != nil {
		params.N = openai.Int(int64(*config.NumberOfResponses))
	}
	if config.PresencePenalty != nil {
		params.PresencePenalty = openai.Float(float64(*config.PresencePenalty))
	}
	if config.FrequencyPenalty != nil {
		params.FrequencyPenalty = openai.Float(float64(*config.FrequencyPenalty))
	}

	if config.ToolChoice != "" {
		if config.ForcedTool != "" {
			params.ToolChoice = openai.F[openai.ChatCompletionToolChoiceOptionUnionParam](
				openai.ChatCompletionNamedToolChoiceParam{
					Type: openai.F(openai.ChatCompletionNamedToolChoiceTypeFunction),
					Function: openai.F(openai.ChatCompletionNamedToolChoiceFunctionParam{
						Name: openai.F(config.ForcedTool),
					}),
				},
			)
		} else {
			params.ToolChoice = openai.F[openai.ChatCompletionToolChoiceOptionUnionParam](
				openai.ChatCompletionToolChoiceOptionBehavior(config.ToolChoice),
			)
		}
	}

	resp, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return Message{}, TokenUsage{}, err
	}

	if len(resp.Choices) == 0 {
		return Message{}, TokenUsage{}, fmt.Errorf("no response generated")
	}

	tokenUsage := TokenUsage{
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: int(resp.Usage.CompletionTokens),
	}

	var contentBlocks []ContentBlock
	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: choice.Message.Content})
		}
		if len(choice.Message.ToolCalls) > 0 {
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

// convertToOpenAIMessages converts internal Message type to OpenAI's ChatCompletionMessageParamUnion
func convertToOpenAIMessages(systemPrompt string, messages []Message) []openai.ChatCompletionMessageParamUnion {
	l := len(messages)
	if systemPrompt != "" {
		l += 1
	}
	openAIMessages := make([]openai.ChatCompletionMessageParamUnion, l)
	if l > len(messages) {
		openAIMessages[0] = openai.SystemMessage(systemPrompt)
	}

	for m, i := 0, l-len(messages); i < l; m, i = m+1, i+1 {
		msg := messages[m]
		switch msg.Role {
		case "assistant":
			var toolCalls []openai.ChatCompletionMessageToolCallParam
			var content string

			for _, contentBlock := range msg.Content {
				switch contentBlock.Type {
				case "text":
					content = contentBlock.Text
				case "tool_use":
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
						ID:   openai.F(contentBlock.ToolUse.ID),
						Type: openai.F(openai.ChatCompletionMessageToolCallTypeFunction),
						Function: openai.F(openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      openai.F(contentBlock.ToolUse.Name),
							Arguments: openai.F(string(contentBlock.ToolUse.Input)),
						}),
					})
				}
			}

			assistantMsg := openai.ChatCompletionAssistantMessageParam{
				Role: openai.F(openai.ChatCompletionAssistantMessageParamRoleAssistant),
				Content: openai.F([]openai.ChatCompletionAssistantMessageParamContentUnion{
					openai.TextPart(content),
				}),
			}
			if len(toolCalls) > 0 {
				assistantMsg.ToolCalls = openai.F(toolCalls)
			}
			openAIMessages[i] = assistantMsg

		case "user":
			for _, contentBlock := range msg.Content {
				switch contentBlock.Type {
				case "tool_result":
					content, err := json.Marshal(struct {
						Content interface{} `json:"content"`
						Error   bool        `json:"error"`
					}{
						contentBlock.ToolResult.Content,
						contentBlock.ToolResult.IsError,
					})
					if err != nil {
						panic(fmt.Sprintf("error marshalling tool result: %v", err))
					}
					openAIMessages[i] = openai.ToolMessage(contentBlock.ToolResult.ToolUseID, string(content))
				case "text":
					openAIMessages[i] = openai.UserMessage(contentBlock.Text)
				}
			}

		}
	}
	return openAIMessages
}

// convertToOpenAITools converts internal Tool type to OpenAI's ChatCompletionToolParam
func convertToOpenAITools(tools []Tool) []openai.ChatCompletionToolParam {
	openAITools := make([]openai.ChatCompletionToolParam, len(tools))
	for i, tool := range tools {
		openAITools[i] = openai.ChatCompletionToolParam{
			Type: openai.F(openai.ChatCompletionToolTypeFunction),
			Function: openai.F(openai.FunctionDefinitionParam{
				Name:        openai.F(tool.Name),
				Description: openai.F(tool.Description),
				Parameters:  openai.F(openai.FunctionParameters(tool.InputSchema)),
			}),
		}
	}
	return openAITools
}
