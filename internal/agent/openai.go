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
	"strings"
	"time"
)

func init() {
	// Register OpenAI types with gob
	gob.Register(oai.SystemMessage(""))
	gob.Register(oai.UserMessage(""))
	gob.Register(oai.AssistantMessage(""))
	gob.Register(oai.ToolMessage("", ""))
	gob.Register(oai.ChatCompletionMessageParam{})
	gob.Register(oai.ChatCompletionAssistantMessageParam{})
	gob.Register(oai.ChatCompletionMessageToolCallParam{})
	gob.Register(oai.ChatCompletionMessageToolCallFunctionParam{})
	gob.Register(oai.ChatCompletionUserMessageParam{})
	gob.Register(oai.ChatCompletionToolMessageParam{})
	gob.Register(oai.ChatCompletionContentPartTextParam{})
	gob.Register([]oai.ChatCompletionMessageParamUnion{})
	gob.Register([]oai.ChatCompletionMessageToolCallParam{})
	gob.Register([]oai.ChatCompletionContentPartUnionParam{})
	gob.Register([]oai.ChatCompletionContentPartTextParam{})
	gob.Register([]oai.ChatCompletionAssistantMessageParamContentUnion{})
	gob.Register(map[string]interface{}{})
}

type openaiExecutor struct {
	client  *oai.Client
	logger  Logger
	ignorer *gitignore.GitIgnore
	config  GenConfig
	params  *oai.ChatCompletionNewParams
}

func NewOpenAIExecutor(baseUrl string, apiKey string, logger Logger, ignorer *gitignore.GitIgnore, config GenConfig) Executor {
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
			{
				Type: oai.F(oai.ChatCompletionToolTypeFunction),
				Function: oai.F(oai.FunctionDefinitionParam{
					Name:        oai.F(changeDirectoryTool.Name),
					Description: oai.F(changeDirectoryTool.Description),
					Parameters:  oai.F(oai.FunctionParameters(changeDirectoryTool.InputSchema)),
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

	return &openaiExecutor{
		client:  client,
		logger:  logger,
		ignorer: ignorer,
		config:  config,
		params:  params,
	}
}

func (o *openaiExecutor) Execute(input string) error {
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
			o.logger.Println(choice.Message.Content)
			assistantMsg = append(assistantMsg, oai.AssistantMessage(choice.Message.Content))
		}

		// If no tool calls, add message and finish
		if len(choice.Message.ToolCalls) == 0 {
			o.params.Messages = oai.F(append(o.params.Messages.Value, assistantMsg...))
			break
		}

		// Process tool calls
		for _, toolCall := range choice.Message.ToolCalls {
			o.logger.Printf("Tool: %s", toolCall.Function.Name)

			var result *ToolResult

			switch toolCall.Function.Name {
			case bashTool.Name:
				var bashToolInput struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &bashToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal bash tool arguments: %w", err)
				}
				o.logger.Printf("executing bash command: %s", bashToolInput.Command)
				result, err = executeBashTool(bashToolInput.Command)
			case fileEditor.Name:
				var fileEditorToolInput FileEditorParams
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &fileEditorToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal file editor tool arguments: %w", err)
				}
				o.logger.Printf(
					"executing file editor tool; command: %s\npath: %s\nold_str: %s\nnew_str: %s",
					fileEditorToolInput.Command,
					fileEditorToolInput.Path,
					fileEditorToolInput.OldStr,
					fileEditorToolInput.NewStr,
				)
				result, err = executeFileEditorTool(fileEditorToolInput)
			case filesOverviewTool.Name:
				o.logger.Println("executing files overview tool")
				result, err = executeFilesOverviewTool(o.ignorer)
			case getRelatedFilesTool.Name:
				var relatedFilesToolInput struct {
					InputFiles []string `json:"input_files"`
				}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &relatedFilesToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal get related files tool arguments: %w", err)
				}
				o.logger.Printf("getting related files: %s", strings.Join(relatedFilesToolInput.InputFiles, ", "))
				result, err = executeGetRelatedFilesTool(relatedFilesToolInput.InputFiles, o.ignorer)
			case changeDirectoryTool.Name:
				var changeDirToolInput struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &changeDirToolInput); err != nil {
					return fmt.Errorf("failed to unmarshal change directory tool arguments: %w", err)
				}
				o.logger.Printf("changing directory to: %s", changeDirToolInput.Path)
				result, err = executeChangeDirectoryTool(changeDirToolInput.Path)
			default:
				return fmt.Errorf("unexpected tool name: %s", toolCall.Function.Name)
			}

			if err != nil {
				return fmt.Errorf("failed to execute tool %s: %w", toolCall.Function.Name, err)
			}

			resultStr := fmt.Sprintf("tool result: %+v", result.Content)
			
			// Truncate result if needed (use 1/4 of model's max tokens as limit for tool results)
			maxResultTokens := o.config.MaxTokens / 4
			truncatedResult, err := truncateResult(resultStr, maxResultTokens, o.config.Model)
			if err != nil {
				return fmt.Errorf("failed to truncate tool result: %w", err)
			}
			
			o.logger.Println(truncatedResult)
			result.Content = truncatedResult
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

			assistantMsg = append(assistantMsg, oai.ToolMessage(toolCall.ID, string(content)))
		}

		// Add messages and continue conversation
		o.params.Messages = oai.F(append(o.params.Messages.Value, assistantMsg...))
	}

	return nil
}

func (o *openaiExecutor) LoadMessages(r io.Reader) error {
	var convo Conversation[[]oai.ChatCompletionMessageParamUnion]
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&convo); err != nil {
		return fmt.Errorf("failed to decode conversation: %w", err)
	}
	o.params.Messages = oai.F(convo.Messages)
	return nil
}

func (o *openaiExecutor) SaveMessages(w io.Writer) error {
	convo := Conversation[[]oai.ChatCompletionMessageParamUnion]{
		Type:     "openai",
		Messages: o.params.Messages.Value,
	}
	enc := gob.NewEncoder(w)
	if err := enc.Encode(convo); err != nil {
		return fmt.Errorf("failed to encode conversation: %w", err)
	}
	return nil
}
