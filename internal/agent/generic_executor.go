package agent

import (
	"fmt"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/llm"
	"log/slog"
)

type genericExecutor struct {
	provider  llm.LLMProvider
	genConfig llm.GenConfig
	logger    *slog.Logger
	ignorer   *gitignore.GitIgnore
}

func NewGenericExecutor(provider llm.LLMProvider, genConfig llm.GenConfig, logger *slog.Logger, ignorer *gitignore.GitIgnore) Executor {
	return &genericExecutor{
		provider:  provider,
		genConfig: genConfig,
		logger:    logger,
		ignorer:   ignorer,
	}
}

func (a *genericExecutor) Execute(input string) error {
	conversation := llm.Conversation{
		SystemPrompt: agentInstructions,
		Messages: []llm.Message{
			{
				Role: "user",
				Content: []llm.ContentBlock{
					{Type: "text", Text: input},
				},
			},
		},
		Tools: []llm.Tool{bashTool, fileEditor, filesOverviewTool, getRelatedFilesTool},
	}

	for {
		// Get response from LLM provider
		response, _, err := a.provider.GenerateResponse(a.genConfig, conversation)
		if err != nil {
			return fmt.Errorf("failed to generate response: %w", err)
		}

		// Add assistant's response to conversation history
		conversation.Messages = append(conversation.Messages, response)

		// Process each content block in the response
		for _, block := range response.Content {
			// Handle different block types
			switch block.Type {
			case "text":
				a.logger.Info(block.Text)
			case "tool_use":
				if block.ToolUse == nil {
					continue
				}

				// Print out the tool call block - we'll log the specific parameters in each tool's function
				a.logger.Info("calling tool", slog.String("name", block.ToolUse.Name))

				var result *llm.ToolResult

				// Execute the appropriate tool based on name
				switch block.ToolUse.Name {
				case "bash":
					result, err = executeBashTool(block.ToolUse.Input, a.logger)
				case "file_editor":
					result, err = executeFileEditorTool(block.ToolUse.Input, a.logger)
				case "files_overview":
					result, err = executeFilesOverviewTool(a.ignorer, a.logger)
				case "get_related_files":
					result, err = executeGetRelatedFilesTool(block.ToolUse.Input, a.ignorer, a.logger)
				default:
					return fmt.Errorf("unknown tool: %s", block.ToolUse.Name)
				}

				if err != nil {
					return fmt.Errorf("failed to execute tool %s: %w", block.ToolUse.Name, err)
				}

				// Log bash command output if this was a bash command
				if block.ToolUse.Name == "bash" {
					if result.IsError {
						a.logger.Error("bash command output", slog.String("output", result.Content.(string)))
					} else {
						a.logger.Info("bash command output", slog.String("output", result.Content.(string)))
					}
				}

				// Add tool result to conversation
				result.ToolUseID = block.ToolUse.ID
				conversation.Messages = append(conversation.Messages, llm.Message{
					Role: "user",
					Content: []llm.ContentBlock{
						{
							Type:       "tool_result",
							ToolResult: result,
						},
					},
				})
			}
		}

		// Check if the response has no tool calls, which means we're done
		hasToolCalls := false
		for _, block := range response.Content {
			if block.Type == "tool_use" && block.ToolUse != nil {
				hasToolCalls = true
				break
			}
		}

		if !hasToolCalls {
			break
		}
	}

	return nil
}
