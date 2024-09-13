package main

import (
	"bufio"
	_ "embed"
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

	// Build system message
	systemMessage, err := buildSystemMessage()
	if err != nil {
		fmt.Println("Error building system message:", err)
		return
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
		Messages:     []llm.Message{},
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

	// Add user message to the conversation
	conversation.Messages = append(conversation.Messages, llm.Message{Role: "user", Content: content})

	// Generate response
	response, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		fmt.Println("Error generating response:", err)
		return
	}

	fmt.Println("Response generated successfully:")
	fmt.Println("\n--- Full Content ---")
	fmt.Println(response)
	fmt.Println("--- End of Content ---")

	// Parse modifications
	modifications, err := parser.ParseModifications(response)
	if err != nil {
		fmt.Printf("Error parsing modifications: %v\n", err)
		return
	}

	// Execute file operations
	fileops.ExecuteFileOperations(modifications)
}
