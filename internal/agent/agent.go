package agent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/extract"
	"github.com/spachava753/cpe/internal/fileops"
	"github.com/spachava753/cpe/internal/llm"
	"github.com/spachava753/cpe/internal/typeresolver"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
)

//go:embed agent_instructions.txt
var agentInstructions string

var bashTool = llm.Tool{
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
}

var fileEditor = llm.Tool{
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
				"description": `The commands to run. Allowed options are: "create", "create", "str_replace", "remove".`,
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
}

var filesOverviewTool = llm.Tool{
	Name: "files_overview",
	Description: `A tool to get an overview of all of the files found recursively in the current directory 
* Each file is recursively listed with its relative path from the current directory and the contents of the file.
* The contents of the file may omit certain lines to reduce the number of lines returned. For example, for source code files, the function and method bodies are omitted.
* The file can be of any type, as long as it contains only text`,
	InputSchema: map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	},
}

var getRelatedFilesTool = llm.Tool{
	Name: "get_related_files",
	Description: `A tool to help retrieve relevant files for a given set of input files
* If the input files contain source code files, symbols like functions and types are extracted and matched in other files that contain the symbol's definition
* If the input files contain other files, the tool will try to find files that mention the input files' names
* This tool should only be called after the "files_overview" tool`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input_files": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": `An array of input files to retrieve related files, e.g. source code files that have symbol definitions in another file or other files that mention the file's name.'`,
			},
		},
		"required": []string{
			"input_files",
		},
	},
}

type Agent struct {
	Provider  llm.LLMProvider
	GenConfig llm.GenConfig
	Logger    *slog.Logger
	Ignorer   *gitignore.GitIgnore
}

// executeBashTool validates and executes the bash tool
func (a Agent) executeBashTool(input json.RawMessage) (*llm.ToolResult, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("invalid bash tool input: %w", err)
	}

	if params.Command == "" {
		return nil, fmt.Errorf("command parameter is required")
	}

	// Execute the bash command
	cmd := exec.Command("bash", "-c", params.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &llm.ToolResult{
			Content: string(output),
			IsError: true,
		}, nil
	}

	return &llm.ToolResult{
		Content: string(output),
		IsError: false,
	}, nil
}

// executeFileEditorTool validates and executes the file editor tool
func (a Agent) executeFileEditorTool(input json.RawMessage) (*llm.ToolResult, error) {
	var params struct {
		Command  string `json:"command"`
		Path     string `json:"path"`
		FileText string `json:"file_text,omitempty"`
		OldStr   string `json:"old_str,omitempty"`
		NewStr   string `json:"new_str,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("invalid file editor tool input: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path parameter is required")
	}

	var mods []extract.Modification
	switch params.Command {
	case "create":
		if params.FileText == "" {
			return nil, fmt.Errorf("file_text parameter is required for create command")
		}
		mods = append(mods, extract.CreateFile{
			Path:    params.Path,
			Content: params.FileText,
		})
	case "str_replace":
		if params.OldStr == "" {
			return nil, fmt.Errorf("old_str parameter is required for str_replace command")
		}
		mods = append(mods, extract.ModifyFile{
			Path: params.Path,
			Edits: []extract.Edit{
				{
					Search:  params.OldStr,
					Replace: params.NewStr,
				},
			},
		})
	case "remove":
		mods = append(mods, extract.RemoveFile{
			Path: params.Path,
		})
	default:
		return nil, fmt.Errorf("invalid command: %s", params.Command)
	}

	if len(mods) == 0 {
		return &llm.ToolResult{
			Content: "at least one modification action is required",
			IsError: true,
		}, nil
	}

	results := fileops.ExecuteFileOperations(mods)
	if len(results) == 0 {
		return &llm.ToolResult{
			Content: "no modifications applied",
			IsError: false,
		}, nil
	}

	// Check if any operation failed
	var errs []string
	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, fmt.Sprintf("failed to execute %T on %s: %v", result.Operation, params.Path, result.Error))
		}
	}

	if len(errs) > 0 {
		return &llm.ToolResult{
			Content: strings.Join(errs, "\n"),
			IsError: true,
		}, nil
	}

	return &llm.ToolResult{
		Content: fmt.Sprintf("successfully executed %s command on %s", params.Command, params.Path),
		IsError: false,
	}, nil
}

// executeFilesOverviewTool validates and executes the files overview tool
func (a Agent) executeFilesOverviewTool(input json.RawMessage) (*llm.ToolResult, error) {
	// Use the codemap package to generate output
	results, err := codemap.GenerateOutput(os.DirFS("."), 1000, a.Ignorer)
	if err != nil {
		return nil, fmt.Errorf("failed to generate files overview: %w", err)
	}

	// Format the results
	var output strings.Builder
	for _, result := range results {
		output.WriteString(fmt.Sprintf("File: %s\n", result.Path))
		output.WriteString("Content:\n```")
		output.WriteString(result.Content)
		output.WriteString("```\n\n")
	}

	return &llm.ToolResult{
		Content: output.String(),
		IsError: false,
	}, nil
}

// executeGetRelatedFilesTool validates and executes the get related files tool
func (a Agent) executeGetRelatedFilesTool(input json.RawMessage) (*llm.ToolResult, error) {
	var params struct {
		InputFiles []string `json:"input_files"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("invalid get related files tool input: %w", err)
	}

	if len(params.InputFiles) == 0 {
		return nil, fmt.Errorf("input_files parameter is required")
	}

	// Use the typeresolver package to find related files
	relatedFiles, err := typeresolver.ResolveTypeAndFunctionFiles(params.InputFiles, os.DirFS("."), a.Ignorer)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve related files: %w", err)
	}

	// Convert map to sorted slice for consistent output
	var files []string
	for file := range relatedFiles {
		files = append(files, file)
	}
	sort.Strings(files)

	return &llm.ToolResult{
		Content: files,
		IsError: false,
	}, nil
}

func (a Agent) Execute(input string) error {
	// Initialize conversation with available tools and user input
	conversation := llm.Conversation{
		Tools: []llm.Tool{bashTool, fileEditor, filesOverviewTool, getRelatedFilesTool},
		Messages: []llm.Message{
			{
				Role: "user",
				Content: []llm.ContentBlock{
					{
						Type: "text",
						Text: input,
					},
				},
			},
		},
		SystemPrompt: agentInstructions,
	}

	// Start conversation loop
	for {
		// Get response from LLM provider
		response, _, err := a.Provider.GenerateResponse(a.GenConfig, conversation)
		if err != nil {
			return fmt.Errorf("failed to generate response: %w", err)
		}

		// Add assistant's response to conversation history
		conversation.Messages = append(conversation.Messages, response)

		// Process each content block in the response
		for _, block := range response.Content {
			// Handle text content
			if block.Type == "text" {
				a.Logger.Info(block.Text)
			}

			// Handle tool calls
			if block.Type == "tool_use" && block.ToolUse != nil {
				var result *llm.ToolResult

				// Execute the appropriate tool based on name
				switch block.ToolUse.Name {
				case "bash":
					result, err = a.executeBashTool(block.ToolUse.Input)
				case "file_editor":
					result, err = a.executeFileEditorTool(block.ToolUse.Input)
				case "files_overview":
					result, err = a.executeFilesOverviewTool(block.ToolUse.Input)
				case "get_related_files":
					result, err = a.executeGetRelatedFilesTool(block.ToolUse.Input)
				default:
					return fmt.Errorf("unknown tool: %s", block.ToolUse.Name)
				}

				if err != nil {
					return fmt.Errorf("failed to execute tool %s: %w", block.ToolUse.Name, err)
				}

				// Add tool result to conversation
				result.ToolUseID = block.ToolUse.ID
				conversation.Messages = append(conversation.Messages, llm.Message{
					Role: "user",
					Content: []llm.ContentBlock{
						{
							Type:       "tool_result",
							ToolResult: result,
						},
					},
				})
			}
		}

		// Check if the response has no tool calls, which means we're done
		hasToolCalls := false
		for _, block := range response.Content {
			if block.Type == "tool_use" && block.ToolUse != nil {
				hasToolCalls = true
				break
			}
		}

		if !hasToolCalls {
			break
		}
	}

	return nil
}
