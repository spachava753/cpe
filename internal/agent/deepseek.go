package agent

import (
	"context"
	"encoding/json"
	"fmt"
	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"log/slog"
	"strings"
	"time"
)

type deepseekExecutor struct {
	client  *oai.Client
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
}

func NewDeepSeekExecutor(baseUrl string, apiKey string, logger *slog.Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
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
	} else {
		baseUrl = "https://api.deepseek.com/"
	}
	opts = append(opts, option.WithBaseURL(baseUrl))
	client := oai.NewClient(opts...)
	return &deepseekExecutor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
	}
}

func (o *deepseekExecutor) Execute(input string) error {
	params := oai.ChatCompletionNewParams{
		Model:               oai.F(o.config.Model),
		MaxCompletionTokens: oai.Int(int64(o.config.MaxTokens)),
		Temperature:         oai.Float(float64(o.config.Temperature)),
		Tools: oai.F([]oai.ChatCompletionToolParam{
			{
				Type: oai.F(oai.ChatCompletionToolTypeFunction),
				Function: oai.F(oai.FunctionDefinitionParam{
					Name:        oai.F(bashTool.Name),
					Description: oai.F(bashTool.Description),
					Parameters:  oai.F(oai.FunctionParameters(bashTool.InputSchema)),
				}),
			},
			{
				Type: oai.F(oai.ChatCompletionToolTypeFunction),
				Function: oai.F(oai.FunctionDefinitionParam{
					Name:        oai.F(fileEditor.Name),
					Description: oai.F(fileEditor.Description),
					Parameters:  oai.F(oai.FunctionParameters(fileEditor.InputSchema)),
				}),
			},
			{
				Type: oai.F(oai.ChatCompletionToolTypeFunction),
				Function: oai.F(oai.FunctionDefinitionParam{
					Name:        oai.F(filesOverviewTool.Name),
					Description: oai.F(filesOverviewTool.Description),
					Parameters:  oai.F(oai.FunctionParameters(filesOverviewTool.InputSchema)),
				}),
			},
			{
				Type: oai.F(oai.ChatCompletionToolTypeFunction),
				Function: oai.F(oai.FunctionDefinitionParam{
					Name:        oai.F(getRelatedFilesTool.Name),
					Description: oai.F(getRelatedFilesTool.Description),
					Parameters:  oai.F(oai.FunctionParameters(getRelatedFilesTool.InputSchema)),
				}),
			},
		}),
	}

	if o.config.TopP != nil {
		params.TopP = oai.Float(float64(*o.config.TopP))
	}
	if o.config.Stop != nil {
		params.Stop = oai.F[oai.ChatCompletionNewParamsStopUnion](oai.ChatCompletionNewParamsStopArray(o.config.Stop))
	}

	// Add system prompt and user input as messages
	params.Messages = oai.F([]oai.ChatCompletionMessageParamUnion{
		oai.SystemMessage(agentInstructions),
		oai.UserMessage(input),
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
		var assistantMsg []oai.ChatCompletionMessageParamUnion

		// Log any text content
		if choice.Message.Content != "" {
			o.logger.Info(choice.Message.Content)
			assistantMsg = append(assistantMsg, oai.AssistantMessage(choice.Message.Content))
		}

		// If no tool calls, add message and finish
		if len(choice.Message.ToolCalls) == 0 {
			params.Messages = oai.F(append(params.Messages.Value, assistantMsg...))
			break
		}

		// Process tool calls
		for _, toolCall := range choice.Message.ToolCalls {
			o.logger.Info(fmt.Sprintf("%+v", toolCall.Function.Arguments))

			var result *ToolResult

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
			assistantMsg = append(assistantMsg, oai.ChatCompletionAssistantMessageParam{
				Role: oai.F(oai.ChatCompletionAssistantMessageParamRoleAssistant),
				ToolCalls: oai.F([]oai.ChatCompletionMessageToolCallParam{
					{
						ID:   oai.F(toolCall.ID),
						Type: oai.F(oai.ChatCompletionMessageToolCallTypeFunction),
						Function: oai.F(oai.ChatCompletionMessageToolCallFunctionParam{
							Name:      oai.F(toolCall.Function.Name),
							Arguments: oai.F(toolCall.Function.Arguments),
						}),
					},
				}),
			})

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

			// Convert OpenAI tool message to DeepSeek tool message and back
			//toolMsg := oai.ToolMessage(toolCall.ID, string(content))
			toolMsg := oai.ChatCompletionMessageParam{
				Role:       oai.F(oai.ChatCompletionMessageParamRoleTool),
				Content:    oai.F[any](string(content)),
				ToolCallID: oai.F(toolCall.ID),
			}
			assistantMsg = append(assistantMsg, toolMsg)
		}

		// Add messages and continue conversation
		params.Messages = oai.F(append(params.Messages.Value, assistantMsg...))
	}

	return nil
}
