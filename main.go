package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	"github.com/spachava753/cpe/internal/cliopts"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/codemapanalysis"
	"github.com/spachava753/cpe/internal/extract"
	"github.com/spachava753/cpe/internal/fileops"
	"github.com/spachava753/cpe/internal/ignore"
	llm2 "github.com/spachava753/cpe/internal/llm"
	"github.com/spachava753/cpe/internal/tiktokenloader"
	"github.com/spachava753/cpe/internal/tokentree"
	"github.com/spachava753/cpe/internal/typeresolver"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

func logTimeElapsed(start time.Time, operation string) {
	elapsed := time.Since(start)
	fmt.Printf("Time elapsed for %s: %v\n", operation, elapsed)
}

type CodeMap struct {
	XMLName xml.Name `xml:"code_map"`
	Files   []File   `xml:"file"`
}

type File struct {
	Path    string `xml:"path,attr"`
	Content string `xml:",cdata"`
}

func generateCodeMapOutput(maxLiteralLen int, ignoreRules *ignore.Patterns) (string, error) {
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

	codeMap := CodeMap{}
	for _, fileCodeMap := range fileCodeMaps {
		tokens := encoding.Encode(fileCodeMap.Content, nil, nil)
		tokenCount := len(tokens)
		if tokenCount > 1024 {
			fmt.Printf("Warning: Large file detected - %s (%d tokens)\n", fileCodeMap.Path, tokenCount)
		}
		codeMap.Files = append(codeMap.Files, File{
			Path:    fileCodeMap.Path,
			Content: fileCodeMap.Content,
		})
	}

	output, err := xml.MarshalIndent(codeMap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshaling XML: %w", err)
	}

	return string(output), nil
}

func buildSystemMessageWithSelectedFiles(allFiles []string, ignoreRules *ignore.Patterns) (string, error) {
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

	codeMap := CodeMap{}
	for _, filePath := range allFiles {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("error reading file %s: %w", filePath, err)
		}
		codeMap.Files = append(codeMap.Files, File{
			Path:    filePath,
			Content: string(content),
		})
	}

	output, err := xml.MarshalIndent(codeMap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshaling XML: %w", err)
	}

	systemMessage.Write(output)

	return systemMessage.String(), nil
}

func determineCodebaseAccess(provider llm2.LLMProvider, genConfig llm2.GenConfig, userInput string) (bool, error) {
	initialConversation := llm2.Conversation{
		SystemPrompt: InitialPrompt,
		Messages:     []llm2.Message{{Role: "user", Content: []llm2.ContentBlock{{Type: "text", Text: userInput}}}},
		Tools: []llm2.Tool{{
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
			llm2.PrintTokenUsage(tokenUsage)
			return result.RequiresCodebase, nil
		}
	}

	return false, fmt.Errorf("no decision made on codebase access")
}

// getVersion returns the version of the application from build info
func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(unknown version)"
}

func main() {
	startTime := time.Now()
	defer logTimeElapsed(startTime, "entire operation")

	config, err := parseConfig()
	if err != nil {
		handleFatalError(err)
	}

	if config.TokenCountPath != "" {
		if err := tokentree.PrintTokenTree(config.TokenCountPath); err != nil {
			handleFatalError(err)
		}
		return
	}

	provider, genConfig, err := initializeLLMProvider(config)
	if err != nil {
		handleFatalError(err)
	}

	if closer, ok := provider.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	input, err := readInput(config.Input)
	if err != nil {
		handleFatalError(err)
	}

	var requiresCodebase bool
	if config.IncludeFiles != "" {
		// Skip codebase access check if include-files is provided
		requiresCodebase = true
	} else {
		var err error
		requiresCodebase, err = determineCodebaseAccess(provider, genConfig, input)
		if err != nil {
			handleFatalError(err)
		}
	}

	if requiresCodebase {
		err = handleCodebaseSpecificQuery(provider, genConfig, input, config)
		if err != nil {
			handleFatalError(err)
		}
	} else {
		response, tokenUsage, err := generateSimpleResponse(provider, genConfig, input)
		if err != nil {
			handleFatalError(err)
		}
		printResponse(response)
		llm2.PrintTokenUsage(tokenUsage)
	}
}

func parseConfig() (cliopts.Options, error) {
	cliopts.ParseFlags()

	if cliopts.Opts.Version {
		fmt.Printf("cpe version %s\n", getVersion())
		os.Exit(0)
	}

	if cliopts.Opts.Model != "" && cliopts.Opts.Model != llm2.DefaultModel {
		_, ok := llm2.ModelConfigs[cliopts.Opts.Model]
		if !ok && cliopts.Opts.CustomURL == "" {
			return cliopts.Options{}, fmt.Errorf("unknown model '%s' requires -custom-url flag", cliopts.Opts.Model)
		}
	}

	return cliopts.Opts, nil
}

func initializeLLMProvider(config cliopts.Options) (llm2.LLMProvider, llm2.GenConfig, error) {
	return llm2.GetProvider(config.Model, llm2.ModelOptions{
		Model:             config.Model,
		CustomURL:         config.CustomURL,
		MaxTokens:         config.MaxTokens,
		Temperature:       config.Temperature,
		TopP:              config.TopP,
		TopK:              config.TopK,
		FrequencyPenalty:  config.FrequencyPenalty,
		PresencePenalty:   config.PresencePenalty,
		NumberOfResponses: config.NumberOfResponses,
		Debug:             config.Debug,
		Input:             config.Input,
		Version:           config.Version,
		IncludeFiles:      config.IncludeFiles,
	})
}

func readInput(inputPath string) (string, error) {
	readStart := time.Now()
	defer logTimeElapsed(readStart, "reading input")

	var content string
	var err error

	if inputPath == "-" {
		content, err = readFromStdin()
	} else {
		content, err = readFromFile(inputPath)
	}

	if err != nil {
		return "", err
	}

	if len(content) == 0 {
		return "", fmt.Errorf("no input provided. Please provide input via stdin or specify an input file")
	}

	return content, nil
}

func readFromStdin() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	contentBytes, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("error reading from stdin: %w", err)
	}
	return string(contentBytes), nil
}

func readFromFile(filePath string) (string, error) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("error reading from file %s: %w", filePath, err)
	}
	return string(contentBytes), nil
}

func handleCodebaseSpecificQuery(provider llm2.LLMProvider, genConfig llm2.GenConfig, input string, config cliopts.Options) error {
	ignoreRules := ignore.NewIgnoreRules()
	if err := ignoreRules.LoadIgnoreFiles("."); err != nil {
		return fmt.Errorf("error loading .cpeignore files: %w", err)
	}

	codeMapOutput, err := generateCodeMapOutput(100, ignoreRules)
	if err != nil {
		return fmt.Errorf("error generating code map output: %w", err)
	}

	selectedFiles, err := codemapanalysis.PerformAnalysis(provider, genConfig, codeMapOutput, input, os.DirFS("."))
	if err != nil {
		return fmt.Errorf("error performing code map analysis: %w", err)
	}

	if config.IncludeFiles != "" {
		selectedFiles = append(selectedFiles, strings.Split(config.IncludeFiles, ",")...)
	}
	systemMessage, err := buildSystemMessageWithSelectedFiles(selectedFiles, ignoreRules)
	if err != nil {
		return fmt.Errorf("error building system message: %w", err)
	}

	if config.Debug {
		fmt.Println("Generated System Prompt:")
		fmt.Println(systemMessage)
		fmt.Println("--- End of System Prompt ---")
	}

	conversation := llm2.Conversation{
		SystemPrompt: systemMessage,
		Messages:     []llm2.Message{{Role: "user", Content: []llm2.ContentBlock{{Type: "text", Text: input}}}},
	}

	response, tokenUsage, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		return fmt.Errorf("error generating response: %w", err)
	}

	printResponse(response)

	err = handleFileModifications(provider, genConfig, conversation, response)
	if err != nil {
		return fmt.Errorf("error handling file modifications: %w", err)
	}

	llm2.PrintTokenUsage(tokenUsage)
	return nil
}

func handleFileModifications(provider llm2.LLMProvider, genConfig llm2.GenConfig, conversation llm2.Conversation, initialResponse llm2.Message) error {
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		conversation.Messages = append(conversation.Messages, initialResponse)

		modifications, err := extractModifications(initialResponse)
		if err != nil {
			return fmt.Errorf("error parsing modifications: %w", err)
		}

		results := fileops.ExecuteFileOperations(modifications)
		errors := collectErrors(results)

		if len(errors) == 0 {
			fmt.Println("All operations completed successfully.")
			printSummary(results)
			return nil
		}

		if retry < maxRetries-1 {
			fmt.Printf("Errors encountered. Retrying (Attempt %d/%d)...\n", retry+2, maxRetries)
			retryMessage := buildRetryMessage(errors)
			conversation.Messages = append(conversation.Messages, llm2.Message{
				Role:    "user",
				Content: []llm2.ContentBlock{{Type: "text", Text: retryMessage}},
			})
			initialResponse, _, err = provider.GenerateResponse(genConfig, conversation)
			if err != nil {
				return fmt.Errorf("error generating retry response: %w", err)
			}
		} else {
			fmt.Println("Max retries reached. Some operations failed.")
			printSummary(results)
			return fmt.Errorf("failed to complete all file modifications")
		}
	}

	return nil
}

func extractModifications(response llm2.Message) ([]extract.Modification, error) {
	var textContent string
	for _, block := range response.Content {
		if block.Type == "text" {
			textContent += block.Text
		}
	}
	return extract.Modifications(textContent)
}

func generateSimpleResponse(provider llm2.LLMProvider, genConfig llm2.GenConfig, input string) (llm2.Message, llm2.TokenUsage, error) {
	systemMessage := "You are an expert Golang developer with extensive knowledge of software engineering principles, design patterns, and best practices. Your role is to assist users with various aspects of Go programming."
	conversation := llm2.Conversation{
		SystemPrompt: systemMessage,
		Messages:     []llm2.Message{{Role: "user", Content: []llm2.ContentBlock{{Type: "text", Text: input}}}},
	}

	return provider.GenerateResponse(genConfig, conversation)
}

func printResponse(response llm2.Message) {
	for _, block := range response.Content {
		if block.Type == "text" {
			fmt.Print(block.Text)
		}
	}
	fmt.Println()
}

func handleFatalError(err error) {
	fmt.Printf("Fatal error: %v\n", err)
	os.Exit(1)
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

func collectErrors(results []fileops.OperationResult) []string {
	var errors []string
	for _, result := range results {
		if result.Error != nil {
			errors = append(errors, fmt.Sprintf("%s: %s", getOperationDescription(result.Operation), result.Error))
		}
	}
	return errors
}

func buildRetryMessage(errors []string) string {
	errorMessage := "The following errors occurred while attempting to modify the codebase:\n\n"
	for _, err := range errors {
		errorMessage += "- " + err + "\n"
	}
	errorMessage += "\nPlease review these errors and provide updated instructions to resolve them. " +
		"Ensure that the modifications are valid and can be applied to the existing codebase. " +
		"Here's a reminder of the original request:\n\n"

	return errorMessage
}
