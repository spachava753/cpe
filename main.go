package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/spachava753/cpe/codemap"
	"github.com/spachava753/cpe/extract"
	"github.com/spachava753/cpe/fileops"
	"github.com/spachava753/cpe/llm"
	"io"
	"io/fs"
	"os"
	"strings"
)

func generateCodeMapOutput(maxLiteralLen int) (string, error) {
	output, err := codemap.GenerateOutput(os.DirFS("."), maxLiteralLen)
	if err != nil {
		return "", fmt.Errorf("error generating code map output: %w", err)
	}
	return output, nil
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

	printTokenUsage(tokenUsage)

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
			return result.SelectedFiles, nil
		}
	}

	return nil, fmt.Errorf("no files selected for analysis")
}

func resolveTypeAndFunctionFiles(selectedFiles []string, sourceFS fs.FS) (map[string]bool, error) {
	typeDefinitions := make(map[string]map[string]string)     // package.type -> file
	functionDefinitions := make(map[string]map[string]string) // package.function -> file
	usages := make(map[string]bool)
	imports := make(map[string]map[string]string) // file -> alias -> package

	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	// Queries
	typeDefQuery, _ := sitter.NewQuery([]byte(`
                (type_declaration
                    (type_spec
                        name: (type_identifier) @type.definition))
                (type_alias
                    name: (type_identifier) @type.alias.definition)`), golang.GetLanguage())
	funcDefQuery, _ := sitter.NewQuery([]byte(`
                (function_declaration
                        name: (identifier) @function.definition)
                (method_declaration
                        name: (field_identifier) @method.definition)`), golang.GetLanguage())
	importQuery, _ := sitter.NewQuery([]byte(`
                (import_declaration
                    (import_spec_list
                        (import_spec
                            name: (_)? @import.name
                            path: (interpreted_string_literal) @import.path)))
                (import_declaration
                    (import_spec
                        name: (_)? @import.name
                        path: (interpreted_string_literal) @import.path))`), golang.GetLanguage())
	typeUsageQuery, _ := sitter.NewQuery([]byte(`
                [
                    (type_identifier) @type.usage
                    (qualified_type
                        package: (package_identifier) @package
                        name: (type_identifier) @type)
                    (generic_type
                        type: [
                            (type_identifier) @type.usage
                            (qualified_type
                                package: (package_identifier) @package
                                name: (type_identifier) @type)
                        ])
                ]`), golang.GetLanguage())
	funcUsageQuery, _ := sitter.NewQuery([]byte(`
                (call_expression
                    function: [
                        (identifier) @function.usage
                        (selector_expression
                            operand: [
                                (identifier) @package
                                (selector_expression)
                            ]?
                            field: (field_identifier) @method.usage)])`), golang.GetLanguage())

	// Parse all files in the source directory and collect type and function definitions
	err := fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			content, err := fs.ReadFile(sourceFS, path)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", path, err)
			}

			tree, err := parser.ParseCtx(context.Background(), nil, content)
			if err != nil {
				return fmt.Errorf("error parsing file %s: %w", path, err)
			}

			// Extract package name
			pkgNameQuery, _ := sitter.NewQuery([]byte(`(package_clause (package_identifier) @package.name)`), golang.GetLanguage())
			pkgNameCursor := sitter.NewQueryCursor()
			pkgNameCursor.Exec(pkgNameQuery, tree.RootNode())
			pkgName := ""
			if match, ok := pkgNameCursor.NextMatch(); ok {
				for _, capture := range match.Captures {
					pkgName = capture.Node.Content(content)
					break
				}
			}

			if _, ok := typeDefinitions[pkgName]; !ok {
				typeDefinitions[pkgName] = make(map[string]string)
			}
			if _, ok := functionDefinitions[pkgName]; !ok {
				functionDefinitions[pkgName] = make(map[string]string)
			}

			// Process type definitions and aliases
			typeCursor := sitter.NewQueryCursor()
			typeCursor.Exec(typeDefQuery, tree.RootNode())
			for {
				match, ok := typeCursor.NextMatch()
				if !ok {
					break
				}
				for _, capture := range match.Captures {
					typeName := capture.Node.Content(content)
					typeDefinitions[pkgName][typeName] = path
				}
			}

			// Process function definitions
			funcCursor := sitter.NewQueryCursor()
			funcCursor.Exec(funcDefQuery, tree.RootNode())
			for {
				match, ok := funcCursor.NextMatch()
				if !ok {
					break
				}
				for _, capture := range match.Captures {
					functionDefinitions[pkgName][capture.Node.Content(content)] = path
				}
			}

			// Process imports
			importCursor := sitter.NewQueryCursor()
			importCursor.Exec(importQuery, tree.RootNode())
			for {
				match, ok := importCursor.NextMatch()
				if !ok {
					break
				}
				var importName, importPath string
				for _, capture := range match.Captures {
					switch capture.Node.Type() {
					case "identifier":
						importName = capture.Node.Content(content)
					case "interpreted_string_literal":
						importPath = strings.Trim(capture.Node.Content(content), "\"")
					}
				}
				if importName == "" {
					parts := strings.Split(importPath, "/")
					importName = parts[len(parts)-1]
				}
				if imports[path] == nil {
					imports[path] = make(map[string]string)
				}
				imports[path][importName] = importPath
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking source directory: %w", err)
	}

	// Collect type and function usages for selected files
	for _, file := range selectedFiles {
		// Check if the file has a .go extension
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		content, err := fs.ReadFile(sourceFS, file)
		if err != nil {
			return nil, fmt.Errorf("error reading file %s: %w", file, err)
		}

		tree, err := parser.ParseCtx(context.Background(), nil, content)
		if err != nil {
			return nil, fmt.Errorf("error parsing file %s: %w", file, err)
		}

		// Process type usages
		typeUsageCursor := sitter.NewQueryCursor()
		typeUsageCursor.Exec(typeUsageQuery, tree.RootNode())
		for {
			match, ok := typeUsageCursor.NextMatch()
			if !ok {
				break
			}
			var packageName, typeName string
			for _, capture := range match.Captures {
				switch capture.Node.Type() {
				case "package_identifier":
					packageName = capture.Node.Content(content)
				case "type_identifier":
					typeName = capture.Node.Content(content)
				}
			}

			// Check if it's a local type
			for pkg, types := range typeDefinitions {
				if defFile, ok := types[typeName]; ok {
					usages[defFile] = true
					break
				}
				if packageName != "" && pkg == packageName {
					if defFile, ok := types[typeName]; ok {
						usages[defFile] = true
						break
					}
				}
			}

			// If not found as a local type, it might be an imported type
			if packageName != "" {
				if importPath, ok := imports[file][packageName]; ok {
					// Mark only the specific imported type as used
					for pkgName, types := range typeDefinitions {
						if strings.HasSuffix(importPath, pkgName) {
							if defFile, ok := types[typeName]; ok {
								usages[defFile] = true
								break
							}
						}
					}
				}
			}
		}

		// Process function usages
		funcUsageCursor := sitter.NewQueryCursor()
		funcUsageCursor.Exec(funcUsageQuery, tree.RootNode())
		for {
			match, ok := funcUsageCursor.NextMatch()
			if !ok {
				break
			}
			var packageName, funcName string
			for _, capture := range match.Captures {
				switch capture.Node.Type() {
				case "identifier", "package":
					packageName = capture.Node.Content(content)
				case "field_identifier", "function.usage", "method.usage":
					funcName = capture.Node.Content(content)
				}
			}

			// Check if it's a local function
			for pkg, funcs := range functionDefinitions {
				if defFile, ok := funcs[funcName]; ok {
					usages[defFile] = true
					break
				}
				if packageName != "" && pkg == packageName {
					if defFile, ok := funcs[funcName]; ok {
						usages[defFile] = true
						break
					}
				}
			}

			// If not found as a local function, it might be an imported function
			if packageName != "" {
				if importPath, ok := imports[file][packageName]; ok {
					// Mark only the specific imported function as used
					for pkgName, funcs := range functionDefinitions {
						if strings.HasSuffix(importPath, pkgName) {
							if defFile, ok := funcs[funcName]; ok {
								usages[defFile] = true
								break
							}
						}
					}
				}
			}
		}

		// Add files containing function definitions used in this file
		for _, funcs := range functionDefinitions {
			for funcName, defFile := range funcs {
				if strings.Contains(string(content), funcName) {
					usages[defFile] = true
				}
			}
		}

		// Add the selected file to the usages
		usages[file] = true
	}

	return usages, nil
}

func buildSystemMessageWithSelectedFiles(selectedFiles []string, includeFiles []string) (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(CodeAnalysisModificationPrompt)

	// Use the current directory for resolveTypeFiles
	currentDir := "."
	resolvedFiles, err := resolveTypeAndFunctionFiles(selectedFiles, os.DirFS(currentDir))
	if err != nil {
		return "", fmt.Errorf("error resolving type files: %w", err)
	}

	// Debug: Print resolved files
	fmt.Println("Resolved files:")
	for filePath := range resolvedFiles {
		fmt.Printf("  - %s\n", filePath)
	}

	// Add go.mod file to the resolved files
	resolvedFiles["go.mod"] = true

	// Add includeFiles to the resolvedFiles
	for _, filePath := range includeFiles {
		resolvedFiles[filePath] = true
	}

	for filePath := range resolvedFiles {
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
	printTokenUsage(tokenUsage)

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

const version = "0.11.1"

func main() {
	flags := ParseFlags()

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

	if len(content) == 0 {
		fmt.Println("Error: No input provided. Please provide input via stdin or specify an input file.")
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
		// Generate low-fidelity code map output
		maxLiteralLen := 100 // You can adjust this value or make it configurable
		codeMapOutput, err := generateCodeMapOutput(maxLiteralLen)
		if err != nil {
			fmt.Printf("Error generating code map output: %v\n", err)
			return
		}

		// Perform code map analysis and select files
		selectedFiles, err := performCodeMapAnalysis(provider, genConfig, codeMapOutput, content)
		if err != nil {
			fmt.Printf("Error performing code map analysis: %v\n", err)
			return
		}

		// Parse include-files flag
		var includeFiles []string
		if flags.IncludeFiles != "" {
			includeFiles = strings.Split(flags.IncludeFiles, ",")
		}

		// Build system message with selected files and included files
		systemMessage, err = buildSystemMessageWithSelectedFiles(selectedFiles, includeFiles)
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
	response, tokenUsage, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		fmt.Println("Error generating response:", err)
		return
	}

	for _, block := range response.Content {
		if block.Type == "text" {
			fmt.Print(block.Text)
		}
	}

	// Print token usage
	printTokenUsage(tokenUsage)

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
		fileops.ExecuteFileOperations(modifications)
	}
}
