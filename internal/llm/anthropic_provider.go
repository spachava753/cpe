package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicProvider struct {
	client *anthropic.Client
}

func NewAnthropicProvider(apiKey string, baseURL string) *AnthropicProvider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(5),
		option.WithRequestTimeout(5 * time.Minute),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
	}
}

func (a *AnthropicProvider) convertToAnthropicTools(tools []Tool) []anthropic.ToolParam {
	anthropicTools := make([]anthropic.ToolParam, len(tools))
	for i, tool := range tools {
		anthropicTools[i] = anthropic.ToolParam{
			Name:        anthropic.F(tool.Name),
			Description: anthropic.F(tool.Description),
			InputSchema: anthropic.F[interface{}](tool.InputSchema),
		}
	}
	return anthropicTools
}

func (a *AnthropicProvider) convertToAnthropicMessages(messages []Message) []anthropic.MessageParam {
	anthropicMessages := make([]anthropic.MessageParam, len(messages))
	for i, msg := range messages {
		var contentBlocks []anthropic.ContentBlockParamUnion

		for _, content := range msg.Content {
			switch content.Type {
			case "text":
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(content.Text))
			case "tool_use":
				if content.ToolUse != nil {
					contentBlocks = append(contentBlocks, anthropic.NewToolUseBlockParam(
						content.ToolUse.ID,
						content.ToolUse.Name,
						content.ToolUse.Input,
					))
				}
			case "tool_result":
				if content.ToolResult != nil {
					contentBlocks = append(contentBlocks, anthropic.NewToolResultBlock(
						content.ToolResult.ToolUseID,
						fmt.Sprintf("%v", content.ToolResult.Content),
						content.ToolResult.IsError,
					))
				}
			}
		}

		if msg.Role == "user" {
			anthropicMessages[i] = anthropic.NewUserMessage(contentBlocks...)
		} else {
			anthropicMessages[i] = anthropic.NewAssistantMessage(contentBlocks...)
		}
	}
	return anthropicMessages
}

func (a *AnthropicProvider) GenerateResponse(config GenConfig, conversation Conversation) (Message, TokenUsage, error) {
	var toolChoice anthropic.ToolChoiceUnionParam
	if config.ToolChoice != "" {
		switch config.ToolChoice {
		case "auto":
			toolChoice = anthropic.ToolChoiceAutoParam{
				Type: anthropic.F(anthropic.ToolChoiceAutoTypeAuto),
			}
		case "any":
			toolChoice = anthropic.ToolChoiceAnyParam{
				Type: anthropic.F(anthropic.ToolChoiceAnyTypeAny),
			}
		case "tool":
			toolChoice = anthropic.ToolChoiceToolParam{
				Type: anthropic.F(anthropic.ToolChoiceToolTypeTool),
				Name: anthropic.F(config.ForcedTool),
			}
		}
	}

	params := anthropic.MessageNewParams{
		Model:       anthropic.F(config.Model),
		MaxTokens:   anthropic.F(int64(config.MaxTokens)),
		Messages:    anthropic.F(a.convertToAnthropicMessages(conversation.Messages)),
		Temperature: anthropic.F(float64(config.Temperature)),
		Tools:       anthropic.F(a.convertToAnthropicTools(conversation.Tools)),
	}

	if conversation.SystemPrompt != "" {
		params.System = anthropic.F([]anthropic.TextBlockParam{
			anthropic.NewTextBlock(conversation.SystemPrompt),
		})
	}

	if config.TopP != nil {
		params.TopP = anthropic.F(float64(*config.TopP))
	}
	if config.TopK != nil {
		params.TopK = anthropic.F(int64(*config.TopK))
	}
	if config.Stop != nil {
		params.StopSequences = anthropic.F(config.Stop)
	}
	if toolChoice != nil {
		params.ToolChoice = anthropic.F(toolChoice)
	}

	resp, err := a.client.Messages.New(context.Background(), params)
	if err != nil {
		return Message{}, TokenUsage{}, fmt.Errorf("error generating response: %w", err)
	}

	tokenUsage := TokenUsage{
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}

	var contentBlocks []ContentBlock
	for _, content := range resp.Content {
		switch block := content.AsUnion().(type) {
		case anthropic.TextBlock:
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: block.Text})
		case anthropic.ToolUseBlock:
			toolUse := &ToolUse{
				ID:    block.ID,
				Name:  block.Name,
				Input: json.RawMessage(block.Input),
			}
			contentBlocks = append(contentBlocks, ContentBlock{Type: "tool_use", ToolUse: toolUse})
		}
	}

	if len(contentBlocks) > 0 {
		return Message{
			Role:    "assistant",
			Content: contentBlocks,
		}, tokenUsage, nil
	}

	return Message{}, TokenUsage{}, fmt.Errorf("no content in response")
}
