package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	"github.com/spachava753/cpe/codemap"
	"github.com/spachava753/cpe/extract"
	"github.com/spachava753/cpe/fileops"
	"github.com/spachava753/cpe/ignore"
	"github.com/spachava753/cpe/llm"
	"github.com/spachava753/cpe/tiktokenloader"
	"github.com/spachava753/cpe/typeresolver"
	"io"
	"os"
	"strings"
	"time"
)

func logTimeElapsed(start time.Time, operation string) {
	elapsed := time.Since(start)
	fmt.Printf("Time elapsed for %s: %v\n", operation, elapsed)
}

func generateCodeMapOutput(maxLiteralLen int, ignoreRules *ignore.IgnoreRules) (string, error) {
	fileCodeMaps, err := codemap.GenerateOutput(os.DirFS("."), maxLiteralLen, ignoreRules)
	if err != nil {
		return "", fmt.Errorf("error generating code map output: %w", err)
	}

	// Initialize tiktoken
	loader := tiktokenloader.NewOfflineLoader()
	tiktoken.SetBpeLoader(loader)
	encoding, err := tiktoken.GetEncoding("o200k_base")
	if err != nil {
		return "", fmt.Errorf("error initializing tiktoken: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("<code_map>\n")
	for _, fileCodeMap := range fileCodeMaps {
		tokens := encoding.Encode(fileCodeMap.Content, nil, nil)
		tokenCount := len(tokens)
		if tokenCount > 1024 {
			fmt.Printf("Warning: Large file detected - %s (%d tokens)\n", fileCodeMap.Path, tokenCount)
		}
		sb.WriteString(fileCodeMap.Content)
	}
	sb.WriteString("</code_map>\n")

	return sb.String(), nil
}

func performCodeMapAnalysis(provider llm.LLMProvider, genConfig llm.GenConfig, codeMapOutput string, userQuery string) ([]string, error) {
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

	genConfig.ToolChoice = "tool"
	genConfig.ForcedTool = "select_files_for_analysis"
	response, tokenUsage, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		return nil, fmt.Errorf("error generating code map analysis: %w", err)
	}

	for _, block := range response.Content {
		if block.Type == "tool_use" && block.ToolUse.Name == "select_files_for_analysis" {
			var result struct {
				Thinking      string   `json:"thinking"`
				SelectedFiles []string `json:"selected_files"`
			}
			if err := json.Unmarshal(block.ToolUse.Input, &result); err != nil {
				return nil, fmt.Errorf("error parsing tool use result: %w", err)
			}
			fmt.Printf("Thinking: %s\nSelected files: %v\n", result.Thinking, result.SelectedFiles)
			printTokenUsage(tokenUsage)
			return result.SelectedFiles, nil
		}
	}

	return nil, fmt.Errorf("no files selected for analysis")
}

func buildSystemMessageWithSelectedFiles(allFiles []string, ignoreRules *ignore.IgnoreRules) (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(CodeAnalysisModificationPrompt)

	// Use the current directory for resolveTypeFiles
	currentDir := "."
	resolvedFiles, err := typeresolver.ResolveTypeAndFunctionFiles(allFiles, os.DirFS(currentDir), ignoreRules)
	if err != nil {
		return "", fmt.Errorf("error resolving type files: %w", err)
	}

	// Debug: Print resolved files
	fmt.Println("Resolved files:")
	for filePath := range resolvedFiles {
		fmt.Printf("  - %s\n", filePath)
	}

	for _, filePath := range allFiles {
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

	genConfig.ToolChoice = "tool"
	genConfig.ForcedTool = "decide_codebase_access"
	response, tokenUsage, err := provider.GenerateResponse(genConfig, initialConversation)
	if err != nil {
		return false, fmt.Errorf("error generating initial response: %w", err)
	}

	fmt.Println(response)
	for _, block := range response.Content {
		if block.Type == "tool_use" && block.ToolUse.Name == "decide_codebase_access" {
			var result struct {
				Thinking         string `json:"thinking"`
				RequiresCodebase bool   `json:"requires_codebase"`
			}
			if err := json.Unmarshal(block.ToolUse.Input, &result); err != nil {
				return false, fmt.Errorf("error parsing tool use result: %w", err)
			}
			fmt.Printf("Thinking: %s\nCodebase access decision: %v\n", result.Thinking, result.RequiresCodebase)
			printTokenUsage(tokenUsage)
			return result.RequiresCodebase, nil
		}
	}

	return false, fmt.Errorf("no decision made on codebase access")
}

func printTokenUsage(usage llm.TokenUsage) {
	fmt.Printf("\n--- Token Usage ---\n")
	fmt.Printf("Input tokens:  %d\n", usage.InputTokens)
	fmt.Printf("Output tokens: %d\n", usage.OutputTokens)
	fmt.Printf("Total tokens:  %d\n", usage.InputTokens+usage.OutputTokens)
	fmt.Printf("-------------------\n")
}

const version = "0.11.6"

func main() {
	startTime := time.Now()
	defer logTimeElapsed(startTime, "entire operation")

	flags := ParseFlags()

	// Initialize ignore rules
	ignoreRules := ignore.NewIgnoreRules()
	if err := ignoreRules.LoadIgnoreFile("."); err != nil {
		fmt.Printf("Error loading .cpeignore file: %v\n", err)
		return
	}

	if flags.Version {
		fmt.Printf("cpe version %s\n", version)
		return
	}

	if flags.Model != "" && flags.Model != defaultModel {
		_, ok := modelConfigs[flags.Model]
		if !ok && flags.CustomURL == "" {
			fmt.Printf("Error: Unknown model '%s' requires -custom-url flag\n", flags.Model)
			flag.Usage()
			os.Exit(1)
		}
	}

	provider, genConfig, err := GetProvider(flags.Model, flags)
	if err != nil {
		fmt.Printf("Error initializing provider: %v\n", err)
		return
	}

	if closer, ok := provider.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	// Read content from input source
	readStart := time.Now()
	var content string
	if flags.Input == "-" {
		// Read from stdin
		reader := bufio.NewReader(os.Stdin)
		contentBytes, readErr := io.ReadAll(reader)
		if readErr != nil {
			fmt.Println("Error reading from stdin:", readErr)
			os.Exit(1)
		}
		content = string(contentBytes)
	} else {
		// Read from file
		contentBytes, readErr := os.ReadFile(flags.Input)
		if readErr != nil {
			fmt.Printf("Error reading from file %s: %v\n", flags.Input, readErr)
			os.Exit(1)
		}
		content = string(contentBytes)
	}
	logTimeElapsed(readStart, "reading input")

	if len(content) == 0 {
		fmt.Println("Error: No input provided. Please provide input via stdin or specify an input file.")
		return
	}

	// Determine if codebase access is required
	codebaseAccessStart := time.Now()
	requiresCodebase, err := determineCodebaseAccess(provider, genConfig, content)
	logTimeElapsed(codebaseAccessStart, "determineCodebaseAccess")
	if err != nil {
		fmt.Printf("Error determining codebase access: %v\n", err)
		return
	}

	var systemMessage string
	if requiresCodebase {
		// Generate low-fidelity code map output
		codeMapStart := time.Now()
		maxLiteralLen := 100 // You can adjust this value or make it configurable
		codeMapOutput, err := generateCodeMapOutput(maxLiteralLen, ignoreRules)
		logTimeElapsed(codeMapStart, "generateCodeMapOutput")
		if err != nil {
			fmt.Printf("Error generating code map output: %v\n", err)
			return
		}

		// Perform code map analysis and select files
		analysisStart := time.Now()
		selectedFiles, err := performCodeMapAnalysis(provider, genConfig, codeMapOutput, content)
		logTimeElapsed(analysisStart, "performCodeMapAnalysis")
		if err != nil {
			fmt.Printf("Error performing code map analysis: %v\n", err)
			return
		}

		// Parse include-files flag
		var includeFiles []string
		if flags.IncludeFiles != "" {
			includeFiles = strings.Split(flags.IncludeFiles, ",")
		}

		// Combine selected and included files
		allFiles := append(selectedFiles, includeFiles...)
		// Build system message with all files
		buildMessageStart := time.Now()
		systemMessage, err = buildSystemMessageWithSelectedFiles(allFiles, ignoreRules)
		logTimeElapsed(buildMessageStart, "buildSystemMessageWithSelectedFiles")
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
	responseStart := time.Now()
	response, tokenUsage, err := provider.GenerateResponse(genConfig, conversation)
	logTimeElapsed(responseStart, "GenerateResponse")
	if err != nil {
		fmt.Println("Error generating response:", err)
		return
	}

	for _, block := range response.Content {
		if block.Type == "text" {
			fmt.Print(block.Text)
		}
	}

	if requiresCodebase {
		// Parse modifications
		var textContent string
		for _, block := range response.Content {
			if block.Type == "text" {
				textContent += block.Text
			}
		}
		modifications, err := extract.Modifications(textContent)
		if err != nil {
			fmt.Printf("Error parsing modifications: %v\n", err)
			return
		}

		// Execute file operations
		results := fileops.ExecuteFileOperations(modifications)

		// Print summary of file operations
		printSummary(results)
	}

	// Print token usage after the response
	printTokenUsage(tokenUsage)
}

func printSummary(results []fileops.OperationResult) {
	successful := 0
	failed := 0

	fmt.Println("\nOperation Summary:")
	for _, result := range results {
		if result.Error == nil {
			successful++
			fmt.Printf("✅ Success: %s - %s\n", result.Operation.Type(), getOperationDescription(result.Operation))
		} else {
			failed++
			fmt.Printf("❌ Failed: %s - %s - Error: %v\n", result.Operation.Type(), getOperationDescription(result.Operation), result.Error)
		}
	}

	fmt.Printf("\nTotal operations: %d\n", len(results))
	fmt.Printf("Successful: %d\n", successful)
	fmt.Printf("Failed: %d\n", failed)
}

func getOperationDescription(op extract.Modification) string {
	switch m := op.(type) {
	case extract.ModifyFile:
		return fmt.Sprintf("Modify %s", m.Path)
	case extract.RemoveFile:
		return fmt.Sprintf("Remove %s", m.Path)
	case extract.CreateFile:
		return fmt.Sprintf("Create %s", m.Path)
	default:
		return "Unknown operation"
	}
}
