package agent

import (
	"context"
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"fmt"
	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"io"
	"log/slog"
	"strings"
	"time"
)

type deepseekExecutor struct {
	client  *oai.Client
	logger  *slog.Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	params  *oai.ChatCompletionNewParams
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

	params := &oai.ChatCompletionNewParams{
		Model:               oai.F(config.Model),
		MaxCompletionTokens: oai.Int(int64(config.MaxTokens)),
		Temperature:         oai.Float(float64(config.Temperature)),
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

	if config.TopP != nil {
		params.TopP = oai.Float(float64(*config.TopP))
	}
	if config.Stop != nil {
		params.Stop = oai.F[oai.ChatCompletionNewParamsStopUnion](oai.ChatCompletionNewParamsStopArray(config.Stop))
	}

	// Add system prompt
	params.Messages = oai.F([]oai.ChatCompletionMessageParamUnion{
		oai.SystemMessage(agentInstructions),
	})

	return &deepseekExecutor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
		params:  params,
	}
}

func (o *deepseekExecutor) Execute(input string) error {
	slog.Info("Note that the current V3 model is not yet perfected, it seems like the instruction following and tool calling performance is not yet tuned.")
	slog.Info("Recommend using this model for one-off tasks like generating git commit messages or bash commands.")

	// Add user input as message
	o.params.Messages = oai.F(append(o.params.Messages.Value, oai.UserMessage(input)))

	for {
		// Create message
		resp, err := o.client.Chat.Completions.New(context.Background(), *o.params)
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
			assistantMsg = append(assistantMsg, oai.ChatCompletionMessageParam{
				Role:    oai.F(oai.ChatCompletionMessageParamRoleAssistant),
				Content: oai.F[any](choice.Message.Content),
			})
		}

		// If no tool calls, add message and finish
		if len(choice.Message.ToolCalls) == 0 {
			o.params.Messages = oai.F(append(o.params.Messages.Value, assistantMsg...))
			break
		}

		// Process tool calls
		for _, toolCall := range choice.Message.ToolCalls {
			o.logger.Info(fmt.Sprintf("Tool: %s", toolCall.Function.Name))

			var result *ToolResult

			switch toolCall.Function.Name {
			case bashTool.Name:
				var bashToolInput struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &bashToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal bash tool arguments: %w", err)
				}
				o.logger.Info(fmt.Sprintf("executing bash command: %s", bashToolInput.Command))
				result, err = executeBashTool(bashToolInput.Command)
			case fileEditor.Name:
				var fileEditorToolInput FileEditorParams
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &fileEditorToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal file editor tool arguments: %w", err)
				}
				o.logger.Info("executing file editor tool",
					slog.String("command", fileEditorToolInput.Command),
					slog.String("path", fileEditorToolInput.Path),
				)
				o.logger.Info(fmt.Sprintf("old_str:\n%s\n\nnew_str:\n%s", fileEditorToolInput.OldStr, fileEditorToolInput.NewStr))
				result, err = executeFileEditorTool(fileEditorToolInput)
			case filesOverviewTool.Name:
				o.logger.Info("executing files overview tool")
				result, err = executeFilesOverviewTool(o.ignorer)
			case getRelatedFilesTool.Name:
				var relatedFilesToolInput struct {
					InputFiles []string `json:"input_files"`
				}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &relatedFilesToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal get related files tool arguments: %w", err)
				}
				o.logger.Info("getting related files", slog.Any("input_files", relatedFilesToolInput.InputFiles))
				result, err = executeGetRelatedFilesTool(relatedFilesToolInput.InputFiles, o.ignorer)
			default:
				return fmt.Errorf("unexpected tool name: %s", toolCall.Function.Name)
			}

			if err != nil {
				return fmt.Errorf("failed to execute tool %s: %w", toolCall.Function.Name, err)
			}

			resultStr := fmt.Sprintf("tool result: %+v", result.Content)
			if len(resultStr) > 10000 {
				resultStr = resultStr[:10000] + "..."
			}
			o.logger.Info(resultStr)

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

			toolMsg := oai.ChatCompletionMessageParam{
				Role:       oai.F(oai.ChatCompletionMessageParamRoleTool),
				Content:    oai.F[any](string(content)),
				ToolCallID: oai.F(toolCall.ID),
			}
			assistantMsg = append(assistantMsg, toolMsg)
		}

		// Add messages and continue conversation
		o.params.Messages = oai.F(append(o.params.Messages.Value, assistantMsg...))
	}

	return nil
}

func (o *deepseekExecutor) LoadMessages(r io.Reader) error {
	var convo Conversation[[]oai.ChatCompletionMessageParamUnion]
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&convo); err != nil {
		return fmt.Errorf("failed to decode conversation: %w", err)
	}
	o.params.Messages = oai.F(convo.Messages)
	return nil
}

func (o *deepseekExecutor) SaveMessages(w io.Writer) error {
	convo := Conversation[[]oai.ChatCompletionMessageParamUnion]{
		Type:     "deepseek",
		Messages: o.params.Messages.Value,
	}
	enc := gob.NewEncoder(w)
	if err := enc.Encode(convo); err != nil {
		return fmt.Errorf("failed to encode conversation: %w", err)
	}
	return nil
}
