package codemapanalysis

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/spachava753/cpe/llm"
)

//go:embed select_files_for_analysis.json
var selectFilesForAnalysisToolDef json.RawMessage

//go:embed code_map_analysis_prompt.txt
var codeMapAnalysisPrompt string

// PerformAnalysis performs code map analysis and returns selected files
func PerformAnalysis(provider llm.LLMProvider, genConfig llm.GenConfig, codeMapOutput string, userQuery string) ([]string, error) {
	conversation := llm.Conversation{
		SystemPrompt: codeMapAnalysisPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "Here's the code map:\n\n" + codeMapOutput + "\n\nUser query: " + userQuery}}},
		},
		Tools: []llm.Tool{{
			Name:        "select_files_for_analysis",
			Description: "Select files for high-fidelity analysis",
			InputSchema: selectFilesForAnalysisToolDef,
		}},
	}

	genConfig.ToolChoice = "tool"
	genConfig.ForcedTool = "select_files_for_analysis"

	maxRetries := 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		response, tokenUsage, err := provider.GenerateResponse(genConfig, conversation)
		if err != nil {
			return nil, fmt.Errorf("error generating code map analysis: %w", err)
		}

		// Add the response to the conversation
		conversation.Messages = append(conversation.Messages, response)

		// We expect only one block, as we are forcing tool use, which prefills the models response, meaning that there should not be any text before the tool call
		if len(response.Content) != 1 {
			return nil, fmt.Errorf("unexpected number of blocks in response, expected 0, got: %d", len(response.Content))
		}
		block := response.Content[0]
		if block.Type == "tool_use" && block.ToolUse.Name == "select_files_for_analysis" {
			var result struct {
				Thinking      string   `json:"thinking"`
				SelectedFiles []string `json:"selected_files"`
			}
			if err := json.Unmarshal(block.ToolUse.Input, &result); err != nil {
				errorMsg := fmt.Sprintf("Error parsing tool use result: %v", err)
				fmt.Printf("Error: %s\n", errorMsg)
				fmt.Printf("Model response:\n%s\n", response)

				if attempt < maxRetries {
					retryMsg := fmt.Sprintf("%s\nThe response did not contain the expected tool use. Please use the 'select_files_for_analysis' tool and provide the required input.", errorMsg)
					conversation.Messages = append(conversation.Messages, llm.Message{
						Role:    "user",
						Content: []llm.ContentBlock{{Type: "text", Text: retryMsg}},
					})
					continue
				}
				return nil, fmt.Errorf("error parsing tool use result %s: %w", block.ToolUse.Input, err)
			}
			fmt.Printf("Thinking: %s\nSelected files: %v\n", result.Thinking, result.SelectedFiles)
			llm.PrintTokenUsage(tokenUsage)
			return result.SelectedFiles, nil
		} else {
			errorMsg := fmt.Sprintf("Unexpected tool use or response format")
			fmt.Printf("Error: %s\n", errorMsg)
			fmt.Printf("Model response:\n%s\n", response)

			if attempt < maxRetries {
				retryMsg := fmt.Sprintf("%s\nPlease use the 'select_files_for_analysis' tool and provide the required input.", errorMsg)
				conversation.Messages = append(conversation.Messages, llm.Message{
					Role:    "user",
					Content: []llm.ContentBlock{{Type: "text", Text: retryMsg}},
				})
				continue
			}
		}
	}

	return nil, fmt.Errorf("no files selected for analysis after %d attempts", maxRetries+1)
}
