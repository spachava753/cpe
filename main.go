package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/spachava753/cpe/codemap"
	"github.com/spachava753/cpe/extract"
	"github.com/spachava753/cpe/fileops"
	"github.com/spachava753/cpe/llm"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"strings"
)

func generateCodeMapOutput() (string, error) {
	output, err := codemap.GenerateOutputFromAST(os.DirFS("."))
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

	genConfig.ToolChoice = "auto"
	response, err := provider.GenerateResponse(genConfig, conversation)
	if err != nil {
		return nil, fmt.Errorf("error generating code map analysis: %w", err)
	}

	for _, block := range response.Content {
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

func resolveTypeFiles(selectedFiles []string, sourceFS fs.FS) (map[string]bool, error) {
	fset := token.NewFileSet()
	typeDefinitions := make(map[string]map[string]string) // package.type -> file
	typeUsages := make(map[string]bool)
	imports := make(map[string]map[string]string) // file -> alias -> package

	// Parse all files in the source directory and collect type definitions
	err := fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			file, err := sourceFS.Open(path)
			if err != nil {
				return fmt.Errorf("error opening file %s: %w", path, err)
			}
			defer file.Close()

			astFile, err := parser.ParseFile(fset, path, file, parser.ParseComments)
			if err != nil {
				return fmt.Errorf("error parsing file %s: %w", path, err)
			}

			pkgName := astFile.Name.Name
			if _, ok := typeDefinitions[pkgName]; !ok {
				typeDefinitions[pkgName] = make(map[string]string)
			}

			ast.Inspect(astFile, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.TypeSpec:
					typeDefinitions[pkgName][x.Name.Name] = path
				case *ast.ImportSpec:
					if imports[path] == nil {
						imports[path] = make(map[string]string)
					}
					importPath := strings.Trim(x.Path.Value, "\"")
					alias := ""
					if x.Name != nil {
						alias = x.Name.Name
					} else {
						parts := strings.Split(importPath, "/")
						alias = parts[len(parts)-1]
					}
					imports[path][alias] = importPath
				}
				return true
			})
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking source directory: %w", err)
	}

	// Collect type usages for selected files
	for _, file := range selectedFiles {
		f, err := sourceFS.Open(file)
		if err != nil {
			return nil, fmt.Errorf("error opening file %s: %w", file, err)
		}
		defer f.Close()

		astFile, err := parser.ParseFile(fset, file, f, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("error parsing file %s: %w", file, err)
		}

		ast.Inspect(astFile, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if ident, ok := x.X.(*ast.Ident); ok {
					if importPath, ok := imports[file][ident.Name]; ok {
						parts := strings.Split(importPath, "/")
						pkgName := parts[len(parts)-1]
						if defFile, ok := typeDefinitions[pkgName][x.Sel.Name]; ok {
							typeUsages[defFile] = true
						}
					}
				}
			case *ast.Ident:
				if defFile, ok := typeDefinitions[astFile.Name.Name][x.Name]; ok {
					typeUsages[defFile] = true
				}
			}
			return true
		})

		// Add the selected file to the type usages
		typeUsages[file] = true
	}

	return typeUsages, nil
}

func buildSystemMessageWithSelectedFiles(selectedFiles []string) (string, error) {
	var systemMessage strings.Builder
	systemMessage.WriteString(CodeAnalysisModificationPrompt)

	// Use the current directory for resolveTypeFiles
	currentDir := "."
	resolvedFiles, err := resolveTypeFiles(selectedFiles, os.DirFS(currentDir))
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

	genConfig.ToolChoice = "auto"
	response, err := provider.GenerateResponse(genConfig, initialConversation)
	if err != nil {
		return false, fmt.Errorf("error generating initial response: %w", err)
	}

	fmt.Println(response)

	for _, block := range response.Content {
		if block.Type == "tool_use" && block.ToolUse.Name == "decide_codebase_access" {
			var result struct {
				RequiresCodebase bool   `json:"requires_codebase"`
				Reason           string `json:"reason"`
			}
			if err := json.Unmarshal(block.ToolUse.Input, &result); err != nil {
				return false, fmt.Errorf("error parsing tool use result: %w", err)
			}
			fmt.Printf("Codebase access decision: %v\nReason: %s\n", result.RequiresCodebase, result.Reason)
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
		codeMapOutput, err := generateCodeMapOutput()
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
		fileops.ExecuteFileOperations(modifications)
	}
}
