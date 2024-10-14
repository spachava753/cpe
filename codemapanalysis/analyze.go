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
		fmt.Printf("Attempt %d of %d\n", attempt+1, maxRetries+1)
		response, tokenUsage, err := provider.GenerateResponse(genConfig, conversation)
		if err != nil {
			return nil, fmt.Errorf("error generating code map analysis: %w", err)
		}

		// Add the response to the conversation
		conversation.Messages = append(conversation.Messages, response)

		// We expect only one block, as we are forcing tool use, which prefills the models response, meaning that there should not be any text before the tool call
		if len(response.Content) != 1 {
			return nil, fmt.Errorf("unexpected number of blocks in response, expected 1, got: %d", len(response.Content))
		}
		block := response.Content[0]
		if block.Type != "tool_use" || block.ToolUse.Name != "select_files_for_analysis" {
			return nil, fmt.Errorf("unexpected response format: expected tool_use with select_files_for_analysis, got %s", response)
		}

		var result struct {
			Thinking      string   `json:"thinking"`
			SelectedFiles []string `json:"selected_files"`
		}
		if unmarshallErr := json.Unmarshal(block.ToolUse.Input, &result); unmarshallErr != nil {
			errorMsg := fmt.Sprintf("Error parsing tool use result: %v", unmarshallErr)
			fmt.Printf("Error: %s\n", errorMsg)
			fmt.Printf("Model response:\n%s\n", response)

			if attempt < maxRetries {
				conversation.Messages = append(conversation.Messages, llm.Message{
					Role: "user",
					Content: []llm.ContentBlock{{
						Type: "tool_result",
						ToolResult: &llm.ToolResult{
							ToolUseID: block.ToolUse.ID,
							Content:   errorMsg,
							IsError:   true,
						},
					}},
				})
				continue
			}
			return nil, fmt.Errorf("error parsing tool use result %s: %w", block.ToolUse.Input, unmarshallErr)
		}
		fmt.Printf("Thinking: %s\nSelected files: %v\n", result.Thinking, result.SelectedFiles)
		llm.PrintTokenUsage(tokenUsage)
		return result.SelectedFiles, nil
	}

	return nil, fmt.Errorf("no files selected for analysis after %d attempts", maxRetries+1)
}
