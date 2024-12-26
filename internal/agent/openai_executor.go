package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/llm"
	"log/slog"
	"strings"
	"time"
)

type openaiExecutor struct {
	client  *openai.Client
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  llm.GenConfig
}

func NewOpenAIExecutor(baseUrl string, apiKey string, logger *slog.Logger, ignorer *gitignore.GitIgnore, config llm.GenConfig) Executor {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(5),
		option.WithRequestTimeout(5 * time.Minute),
	}
	if baseUrl != "" {
		// Ensure baseURL ends with a trailing "/"
		if !strings.HasSuffix(baseUrl, "/") {
			baseUrl = baseUrl + "/"
		}
		opts = append(opts, option.WithBaseURL(baseUrl))
	}
	client := openai.NewClient(opts...)
	return &openaiExecutor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
	}
}

// buildToolCallMessage creates the assistant message for a tool call
func buildToolCallMessage(toolCall openai.ChatCompletionMessageToolCall) openai.ChatCompletionAssistantMessageParam {
	return openai.ChatCompletionAssistantMessageParam{
		Role: openai.F(openai.ChatCompletionAssistantMessageParamRoleAssistant),
		ToolCalls: openai.F([]openai.ChatCompletionMessageToolCallParam{
			{
				ID:   openai.F(toolCall.ID),
				Type: openai.F(openai.ChatCompletionMessageToolCallTypeFunction),
				Function: openai.F(openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      openai.F(toolCall.Function.Name),
					Arguments: openai.F(toolCall.Function.Arguments),
				}),
			},
		}),
	}
}

func (o *openaiExecutor) Execute(input string) error {
	params := openai.ChatCompletionNewParams{
		Model:               openai.F(o.config.Model),
		MaxCompletionTokens: openai.Int(int64(o.config.MaxTokens)),
		Temperature:         openai.Float(float64(o.config.Temperature)),
		Tools: openai.F([]openai.ChatCompletionToolParam{
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.F(bashTool.Name),
					Description: openai.F(bashTool.Description),
					Parameters:  openai.F(openai.FunctionParameters(bashTool.InputSchema)),
				}),
			},
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.F(fileEditor.Name),
					Description: openai.F(fileEditor.Description),
					Parameters:  openai.F(openai.FunctionParameters(fileEditor.InputSchema)),
				}),
			},
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.F(filesOverviewTool.Name),
					Description: openai.F(filesOverviewTool.Description),
					Parameters:  openai.F(openai.FunctionParameters(filesOverviewTool.InputSchema)),
				}),
			},
			{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.F(getRelatedFilesTool.Name),
					Description: openai.F(getRelatedFilesTool.Description),
					Parameters:  openai.F(openai.FunctionParameters(getRelatedFilesTool.InputSchema)),
				}),
			},
		}),
	}

	if o.config.TopP != nil {
		params.TopP = openai.Float(float64(*o.config.TopP))
	}
	if o.config.Stop != nil {
		params.Stop = openai.F[openai.ChatCompletionNewParamsStopUnion](openai.ChatCompletionNewParamsStopArray(o.config.Stop))
	}

	// Add system prompt and user input as messages
	params.Messages = openai.F([]openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(agentInstructions),
		openai.UserMessage(input),
	})

	for {
		// Create message
		resp, err := o.client.Chat.Completions.New(context.Background(), params)
		if err != nil {
			return fmt.Errorf("failed to create message: %w", err)
		}

		if len(resp.Choices) == 0 {
			return fmt.Errorf("no response generated")
		}

		// Get the single choice
		choice := resp.Choices[0]
		var assistantMsg []openai.ChatCompletionMessageParamUnion

		// Log any text content
		if choice.Message.Content != "" {
			o.logger.Info(choice.Message.Content)
			assistantMsg = append(assistantMsg, openai.AssistantMessage(choice.Message.Content))
		}

		// If no tool calls, add message and finish
		if len(choice.Message.ToolCalls) == 0 {
			params.Messages = openai.F(append(params.Messages.Value, assistantMsg...))
			break
		}

		// Process tool calls
		for _, toolCall := range choice.Message.ToolCalls {
			o.logger.Info(fmt.Sprintf("%+v", toolCall.Function.Arguments))

			var result *llm.ToolResult

			switch toolCall.Function.Name {
			case bashTool.Name:
				result, err = executeBashTool(json.RawMessage(toolCall.Function.Arguments), o.logger)
			case fileEditor.Name:
				result, err = executeFileEditorTool(json.RawMessage(toolCall.Function.Arguments), o.logger)
			case filesOverviewTool.Name:
				result, err = executeFilesOverviewTool(o.ignorer, o.logger)
			case getRelatedFilesTool.Name:
				result, err = executeGetRelatedFilesTool(json.RawMessage(toolCall.Function.Arguments), o.ignorer, o.logger)
			default:
				return fmt.Errorf("unexpected tool name: %s", toolCall.Function.Name)
			}

			if err != nil {
				return fmt.Errorf("failed to execute tool %s: %w", toolCall.Function.Name, err)
			}

			result.ToolUseID = toolCall.ID

			// Add assistant message for tool call
			assistantMsg = append(assistantMsg, buildToolCallMessage(toolCall))

			// Marshal tool result
			content, unmarshallErr := json.Marshal(struct {
				Content interface{} `json:"content"`
				Error   bool        `json:"error"`
			}{
				result.Content,
				result.IsError,
			})
			if unmarshallErr != nil {
				return fmt.Errorf("failed to marshal tool result: %w", unmarshallErr)
			}

			assistantMsg = append(assistantMsg, openai.ToolMessage(toolCall.ID, string(content)))
		}

		// Add messages and continue conversation
		params.Messages = openai.F(append(params.Messages.Value, assistantMsg...))
	}

	return nil
}
