package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/cliopts"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/codemapanalysis"
	"github.com/spachava753/cpe/internal/extract"
	"github.com/spachava753/cpe/internal/fileops"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/cpe/internal/llm"
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

func generateCodeMapOutput(maxLiteralLen int, ignorer *gitignore.GitIgnore) (string, error) {
	fileCodeMaps, err := codemap.GenerateOutput(os.DirFS("."), maxLiteralLen, ignorer)
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

func buildSystemMessageWithSelectedFiles(allFiles []string, ignorer *gitignore.GitIgnore) (string, error) {
	var systemMessage strings.Builder

	// Use the current directory for resolveTypeFiles
	currentDir := "."
	resolvedFiles, err := typeresolver.ResolveTypeAndFunctionFiles(allFiles, os.DirFS(currentDir), ignorer)
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

	systemMessage.WriteString("\n\n")
	systemMessage.WriteString(CodeAnalysisModificationPrompt)

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
			llm.PrintTokenUsage(tokenUsage)
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
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		handleFatalError(err)
	}
	if ignorer == nil {
		handleFatalError(errors.New("git ignorer was nil"))
	}

	if config.TokenCountPath != "" {
		if err := tokentree.PrintTokenTree(os.DirFS("."), ignorer); err != nil {
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
		err = handleCodebaseSpecificQuery(provider, genConfig, input, config, ignorer)
		if err != nil {
			handleFatalError(err)
		}
	} else {
		response, tokenUsage, err := generateSimpleResponse(provider, genConfig, input)
		if err != nil {
			handleFatalError(err)
		}
		printResponse(response)
		llm.PrintTokenUsage(tokenUsage)
	}
}

func parseConfig() (cliopts.Options, error) {
	cliopts.ParseFlags()

	if cliopts.Opts.Version {
		fmt.Printf("cpe version %s\n", getVersion())
		os.Exit(0)
	}

	if cliopts.Opts.Model != "" && cliopts.Opts.Model != llm.DefaultModel {
		_, ok := llm.ModelConfigs[cliopts.Opts.Model]
		if !ok && cliopts.Opts.CustomURL == "" {
			return cliopts.Options{}, fmt.Errorf("unknown model '%s' requires -custom-url flag", cliopts.Opts.Model)
		}
	}

	return cliopts.Opts, nil
}

func initializeLLMProvider(config cliopts.Options) (llm.LLMProvider, llm.GenConfig, error) {
	return llm.GetProvider(config.Model, llm.ModelOptions{
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

func handleCodebaseSpecificQuery(provider llm.LLMProvider, genConfig llm.GenConfig, input string, config cliopts.Options, ignorer *gitignore.GitIgnore) error {
	codeMapOutput, err := generateCodeMapOutput(100, ignorer)
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
	systemMessage, err := buildSystemMessageWithSelectedFiles(selectedFiles, ignorer)
	if err != nil {
		return fmt.Errorf("error building system message: %w", err)
	}

	if config.Debug {
		fmt.Println("Generated System Prompt:")
		fmt.Println(systemMessage)
		fmt.Println("--- End of System Prompt ---")
	}

	conversation := llm.Conversation{
		SystemPrompt: systemMessage,
		Messages:     []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: input}}}},
		Tools: []llm.Tool{
			{
				Name: "bash",
				Description: `Run commands in a bash shell
* When invoking this tool, the contents of the "command" parameter does NOT need to be escaped.
* You don't have access to the internet via this tool.
* You do have access to a mirror of common linux and python packages via apt and pip.
* State is persistent across command calls and discussions with the user.
* To inspect a particular line range of a file, e.g. lines 10-25, try 'sed -n 10,25p /path/to/the/file'.
* Avoid commands that may produce a very large amount of output.
* Run long lived commands in the background, e.g. 'sleep 10 &' or start a server in the background`,
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "The bash command to run.",
						},
					},
					"required": []string{"command"},
				},
			},
			{
				Name: "file_editor",
				Description: `A tool to edit, create and delete files
* The "create" command cannot be used if the specified "path" already exists as a file. It should only be used to create a file, and "file_text" must be supplied as the contents of the new file
* The "remove" command can be used to remove an existing file

Notes for using the "str_replace" command:
* The "old_str" parameter should match EXACTLY one or more consecutive lines from the original file. Be mindful of whitespaces!
* If the "old_str" parameter is not unique in the file, the replacement will not be performed. Make sure to include enough context in "old_str" to make it unique
* The "new_str" parameter should contain the edited lines that should replace the "old_str"
* Leave "new_str" parameter empty effectively remove "old_str" text from the file`,
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"create", "str_replace", "remove"},
							"description": `The commands to run. Allowed options are: "view", "create", "str_replace", "insert", "undo_edit".`,
						},
						"file_text": map[string]interface{}{
							"description": `Required parameter of "create" command, with the content of the file to be created.`,
							"type":        "string",
						},
						"new_str": map[string]interface{}{
							"description": `Required parameter of "str_replace" command containing the new string.`,
							"type":        "string",
						},
						"old_str": map[string]interface{}{
							"description": `Required parameter of "str_replace" command containing the string in "path" to replace.`,
							"type":        "string",
						},
						"path": map[string]interface{}{
							"description": `Relative path to file or directory, e.g. "./file.py"`,
							"type":        "string",
						},
					},
					"required": []string{"command", "path"},
				},
			},
		},
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

	llm.PrintTokenUsage(tokenUsage)
	return nil
}

// FileEditorInput represents the input schema for the file_editor tool
type FileEditorInput struct {
	Command  string `json:"command"`             // Required: "create", "str_replace", or "append"
	Path     string `json:"path"`                // Required: relative path to file
	FileText string `json:"file_text,omitempty"` // Required for "create" command
	NewStr   string `json:"new_str,omitempty"`   // Required for "str_replace" and "append" commands
	OldStr   string `json:"old_str,omitempty"`   // Required for "str_replace" command
}

func validateFileEditorInput(input FileEditorInput) error {
	if input.Path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	switch input.Command {
	case "create":
		if input.FileText == "" {
			return fmt.Errorf("file_text is required for create command")
		}
		if input.NewStr != "" || input.OldStr != "" {
			return fmt.Errorf("new_str and old_str should not be provided for create command")
		}
	case "str_replace":
		if input.OldStr == "" {
			return fmt.Errorf("old_str is required for str_replace command")
		}
		if input.FileText != "" {
			return fmt.Errorf("file_text should not be provided for str_replace command")
		}
	case "remove":
		if input.FileText != "" || input.NewStr != "" || input.OldStr != "" {
			return fmt.Errorf("file_text, new_str, and old_str should not be provided for remove command")
		}
	default:
		return fmt.Errorf("invalid command: %s", input.Command)
	}

	return nil
}

func convertFileEditorInputToModification(input FileEditorInput) (extract.Modification, error) {
	switch input.Command {
	case "create":
		return extract.CreateFile{
			Path:        input.Path,
			Content:     input.FileText,
			Explanation: "File creation requested via file_editor tool",
		}, nil
	case "str_replace":
		return extract.ModifyFile{
			Path: input.Path,
			Edits: []extract.Edit{{
				Search:  input.OldStr,
				Replace: input.NewStr,
			}},
			Explanation: "File modification requested via file_editor tool",
		}, nil
	case "remove":
		return extract.RemoveFile{
			Path:        input.Path,
			Explanation: "File removal requested via file_editor tool",
		}, nil
	default:
		return nil, fmt.Errorf("invalid command: %s", input.Command)
	}
}

func handleFileModifications(provider llm.LLMProvider, genConfig llm.GenConfig, conversation llm.Conversation, initialResponse llm.Message) error {
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		conversation.Messages = append(conversation.Messages, initialResponse)

		var modifications []extract.Modification

		// Check for tool use blocks
		for _, block := range initialResponse.Content {
			if block.Type == "tool_use" && block.ToolUse.Name == "file_editor" {
				var input FileEditorInput
				if err := json.Unmarshal(block.ToolUse.Input, &input); err != nil {
					return fmt.Errorf("error unmarshaling file editor input: %w", err)
				}

				// Validate the input
				if err := validateFileEditorInput(input); err != nil {
					return fmt.Errorf("invalid file editor input: %w", err)
				}

				// Convert input to modification
				mod, err := convertFileEditorInputToModification(input)
				if err != nil {
					return fmt.Errorf("error converting file editor input to modification: %w", err)
				}

				modifications = append(modifications, mod)
			}
		}

		results := fileops.ExecuteFileOperations(modifications)
		fileOpErrs := collectErrors(results)

		if len(fileOpErrs) == 0 {
			fmt.Println("All operations completed successfully.")
			printSummary(results)
			return nil
		}

		if retry < maxRetries-1 {
			fmt.Printf("Errors encountered. Retrying (Attempt %d/%d)...\n", retry+2, maxRetries)
			retryMessage := buildRetryMessage(fileOpErrs)
			conversation.Messages = append(conversation.Messages, llm.Message{
				Role:    "user",
				Content: []llm.ContentBlock{{Type: "text", Text: retryMessage}},
			})
			var err error
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

func generateSimpleResponse(provider llm.LLMProvider, genConfig llm.GenConfig, input string) (llm.Message, llm.TokenUsage, error) {
	conversation := llm.Conversation{
		SystemPrompt: GeneralAssistantPrompt,
		Messages:     []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: input}}}},
	}

	return provider.GenerateResponse(genConfig, conversation)
}

func printResponse(response llm.Message) {
	for _, block := range response.Content {
		if block.Type == "text" {
			fmt.Print(block.Text)
		}
	}
	fmt.Println()
}

func handleFatalError(err error) {
	fmt.Printf("fatal error: %v\n", err)
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
