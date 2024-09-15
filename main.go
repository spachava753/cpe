package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gobwas/glob"
	"github.com/spachava753/cpe/codemap"
	"github.com/spachava753/cpe/fileops"
	"github.com/spachava753/cpe/llm"
	"github.com/spachava753/cpe/parser"
	"io"
	"os"
	"strings"
)

func readIgnorePatterns(filename string) ([]glob.Glob, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var patterns []glob.Glob
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			pattern, err := glob.Compile(line)
			if err != nil {
				return nil, fmt.Errorf("invalid pattern %q: %v", line, err)
			}
			patterns = append(patterns, pattern)
		}
	}
	return patterns, nil
}

var ignoreFolders = []string{
	".git",
	"vendor",
	"node_modules",
	".idea",
	".vscode",
	"bin",
	"obj",
	"dist",
	"build",
	"target",
}

func generateCodeMap() (*codemap.CodeMap, error) {
	codebase, err := codemap.ParseCodebase(os.DirFS("."))
	if err != nil {
		return nil, fmt.Errorf("error parsing codebase: %w", err)
	}
	return codebase, nil
}

func performCodeMapAnalysis(provider llm.LLMProvider, genConfig llm.GenConfig, codeMap *codemap.CodeMap, userQuery string) ([]string, error) {
	codeMapOutput := codeMap.GenerateOutput()

	conversation := llm.Conversation{
		SystemPrompt: CodeMapAnalysisPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "Here's the code map:\n\n" + codeMapOutput + "\n\nUser query: " + userQuery}}},
		},
		Tools: []llm.Tool{{
			Name:        "select_files_for_analysis",
			Description: "Select files for high-fidelity analysis",
			InputSchema: SelectFilesForAnalysisToolDef,
		}},
	}

	genConfig.ToolChoice = "auto"
	response, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		return nil, fmt.Errorf("error generating code map analysis: %w", err)
	}

	for _, block := range response {
		if block.Type == "tool_use" && block.ToolUse.Name == "select_files_for_analysis" {
			var result struct {
				SelectedFiles []string `json:"selected_files"`
				Reason        string   `json:"reason"`
			}
			if err := json.Unmarshal(block.ToolUse.Input, &result); err != nil {
				return nil, fmt.Errorf("error parsing tool use result: %w", err)
			}
			fmt.Printf("Selected files: %v\nReason: %s\n", result.SelectedFiles, result.Reason)
			return result.SelectedFiles, nil
		}
	}

	return nil, fmt.Errorf("no files selected for analysis")
}

func buildSystemMessageWithSelectedFiles(selectedFiles []string) (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(SimplePrompt)

	for _, filePath := range selectedFiles {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("error reading file %s: %w", filePath, err)
		}
		systemMessage.WriteString(fmt.Sprintf(`<file>
<path>%s</path>
<code>
%s
</code>
</file>
`, filePath, string(content)))
	}

	return systemMessage.String(), nil
}

func determineCodebaseAccess(provider llm.LLMProvider, genConfig llm.GenConfig, userInput string) (bool, error) {
	initialConversation := llm.Conversation{
		SystemPrompt: InitialPrompt,
		Messages:     []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userInput}}}},
		Tools: []llm.Tool{{
			Name:        "decide_codebase_access",
			Description: "Reports the decision on whether codebase access is required",
			InputSchema: InitialPromptToolCallDef,
		}},
	}

	genConfig.ToolChoice = "auto"
	response, err := provider.GenerateResponse(genConfig, initialConversation)
	if err != nil {
		return false, fmt.Errorf("error generating initial response: %w", err)
	}

	fmt.Println("Initial response generated successfully:")
	fmt.Println(response)

	for _, block := range response {
		if block.Type == "tool_use" && block.ToolUse.Name == "decide_codebase_access" {
			var result struct {
				RequiresCodebase bool   `json:"requires_codebase"`
				Reason           string `json:"reason"`
			}
			if err := json.Unmarshal(block.ToolUse.Input, &result); err != nil {
				return false, fmt.Errorf("error parsing tool use result: %w", err)
			}
			return result.RequiresCodebase, nil
		}
	}

	return false, fmt.Errorf("no decision made on codebase access")
}

func main() {
	flags := ParseFlags()

	if flags.Model != "" && flags.Model != defaultModel {
		_, ok := modelConfigs[flags.Model]
		if !ok && flags.OpenAIURL == "" {
			fmt.Printf("Error: Unknown model '%s' requires -openai-url flag\n", flags.Model)
			flag.Usage()
			os.Exit(1)
		}
	}

	provider, genConfig, err := GetProvider(flags.Model, flags.OpenAIURL, flags)
	if err != nil {
		fmt.Printf("Error initializing provider: %v\n", err)
		return
	}

	if closer, ok := provider.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	// Read content from stdin
	reader := bufio.NewReader(os.Stdin)
	contentBytes, readErr := io.ReadAll(reader)
	if readErr != nil {
		fmt.Println("No input provided")
		os.Exit(1)
	}

	content := string(contentBytes)

	if len(content) == 0 {
		fmt.Println("Error: No input provided. Please provide input via stdin.")
		return
	}

	// Determine if codebase access is required
	requiresCodebase, err := determineCodebaseAccess(provider, genConfig, content)
	if err != nil {
		fmt.Printf("Error determining codebase access: %v\n", err)
		return
	}

	var systemMessage string
	if requiresCodebase {
		// Generate low-fidelity code map
		codeMap, err := generateCodeMap()
		if err != nil {
			fmt.Printf("Error generating code map: %v\n", err)
			return
		}

		// Perform code map analysis and select files
		selectedFiles, err := performCodeMapAnalysis(provider, genConfig, codeMap, content)
		if err != nil {
			fmt.Printf("Error performing code map analysis: %v\n", err)
			return
		}

		// Build system message with selected files
		systemMessage, err = buildSystemMessageWithSelectedFiles(selectedFiles)
		if err != nil {
			fmt.Println("Error building system message:", err)
			return
		}
	} else {
		systemMessage = "You are an expert Golang developer with extensive knowledge of software engineering principles, design patterns, and best practices. Your role is to assist users with various aspects of Go programming."
	}

	// If debug flag is set, print the system message
	if flags.Debug {
		fmt.Println("Generated System Prompt:")
		fmt.Println(systemMessage)
		fmt.Println("--- End of System Prompt ---")
	}

	// Set up the conversation
	conversation := llm.Conversation{
		SystemPrompt: systemMessage,
		Messages:     []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: content}}}},
	}

	// Generate response
	response, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		fmt.Println("Error generating response:", err)
		return
	}

	fmt.Println("Response generated successfully:")
	fmt.Println("\n--- Full Content ---")
	for _, block := range response {
		fmt.Println(block.Text)
	}
	fmt.Println("--- End of Content ---")

	if requiresCodebase {
		// Parse modifications
		modifications, err := parser.ParseModifications(response[0].Text)
		if err != nil {
			fmt.Printf("Error parsing modifications: %v\n", err)
			return
		}

		// Execute file operations
		fileops.ExecuteFileOperations(modifications)
	}
}
