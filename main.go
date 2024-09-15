package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gobwas/glob"
	"github.com/spachava753/cpe/fileops"
	"github.com/spachava753/cpe/llm"
	"github.com/spachava753/cpe/parser"
	"io"
	"os"
	"path/filepath"
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

func buildSystemMessage() (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(SimplePrompt)

	ignorePatterns, err := readIgnorePatterns(".cpeignore")
	if err != nil {
		return "", fmt.Errorf("error reading .cpeignore: %v", err)
	}

	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the current path is in the ignore list
		for _, folder := range ignoreFolders {
			if strings.HasPrefix(path, folder) || strings.Contains(path, "/"+folder+"/") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if !info.IsDir() {
			// Check if the file should be ignored based on .cpeignore patterns
			for _, pattern := range ignorePatterns {
				if pattern.Match(path) {
					return nil // Skip this file
				}
			}

			content, readFileErr := os.ReadFile(path)
			if readFileErr != nil {
				return readFileErr
			}
			systemMessage.WriteString(fmt.Sprintf(`<file>
<path>%s</path>
<code>
%s
</code>
</file>
`, path, string(content)))
		}
		return nil
	})

	if err != nil {
		return "", err
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
		// Build system message with codebase
		systemMessage, err = buildSystemMessage()
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
