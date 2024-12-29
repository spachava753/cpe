package agent

import (
	"fmt"
	ignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/typeresolver"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

var bashTool = Tool{
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

var fileEditor = Tool{
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

var filesOverviewTool = Tool{
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

var getRelatedFilesTool = Tool{
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

type ToolResult struct {
	ToolUseID string
	Content   any
	IsError   bool
}

// executeBashTool validates and executes the bash tool
func executeBashTool(command string, logger *slog.Logger) (*ToolResult, error) {
	logger.Info(fmt.Sprintf("executing bash command: %s", command))

	cmd := exec.Command("bash", "-c", command)
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error executing command: %s\nOutput: %s", err, string(output)),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: string(output),
	}, nil
}

// FileEditorParams represents the parameters for the file editor tool
type FileEditorParams struct {
	Command  string `json:"command"`
	Path     string `json:"path"`
	FileText string `json:"file_text,omitempty"`
	OldStr   string `json:"old_str,omitempty"`
	NewStr   string `json:"new_str,omitempty"`
}

// executeFileEditorTool validates and executes the file editor tool
func executeFileEditorTool(params FileEditorParams, logger *slog.Logger) (*ToolResult, error) {
	logger.Info("executing file editor tool",
		slog.String("command", params.Command),
		slog.String("path", params.Path),
	)

	logger.Info(fmt.Sprintf("old_str:\n%s\n\nnew_str:\n%s", params.OldStr, params.NewStr))

	switch params.Command {
	case "create":
		if params.FileText == "" {
			return &ToolResult{
				Content: "file_text parameter is required for create command",
				IsError: true,
			}, nil
		}
		if err := os.WriteFile(params.Path, []byte(params.FileText), 0644); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error creating file: %s", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Successfully created file %s", params.Path),
		}, nil

	case "str_replace":
		content, err := os.ReadFile(params.Path)
		if err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error reading file: %s", err),
				IsError: true,
			}, nil
		}

		if !strings.Contains(string(content), params.OldStr) {
			return &ToolResult{
				Content: "old_str not found in file",
				IsError: true,
			}, nil
		}

		newContent := strings.Replace(string(content), params.OldStr, params.NewStr, 1)
		if err := os.WriteFile(params.Path, []byte(newContent), 0644); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error writing file: %s", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Successfully replaced text in %s", params.Path),
		}, nil

	case "remove":
		if err := os.Remove(params.Path); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error removing file: %s", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Successfully removed file %s", params.Path),
		}, nil

	default:
		return &ToolResult{
			Content: fmt.Sprintf("Unknown command: %s", params.Command),
			IsError: true,
		}, nil
	}
}

// executeFilesOverviewTool validates and executes the files overview tool
func executeFilesOverviewTool(ignorer *ignore.GitIgnore, logger *slog.Logger) (*ToolResult, error) {
	fsys := os.DirFS(".")
	files, err := codemap.GenerateOutput(fsys, 100, ignorer)
	if err != nil {
		return nil, fmt.Errorf("failed to generate code map: %w", err)
	}

	var sb strings.Builder
	for _, file := range files {
		sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file.Path, file.Content))
	}

	return &ToolResult{
		Content: sb.String(),
	}, nil
}

// executeGetRelatedFilesTool validates and executes the get related files tool
func executeGetRelatedFilesTool(inputFiles []string, ignorer *ignore.GitIgnore, logger *slog.Logger) (*ToolResult, error) {
	logger.Info("getting related files", slog.Any("input_files", inputFiles))

	relatedFiles, err := typeresolver.ResolveTypeAndFunctionFiles(inputFiles, os.DirFS("."), ignorer)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve related files: %w", err)
	}

	// Convert map to sorted slice for consistent output
	var files []string
	for file := range relatedFiles {
		files = append(files, file)
	}
	sort.Strings(files)

	logger.Info("found related files", slog.Any("files", files))

	var sb strings.Builder
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", file, err)
		}
		sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file, string(content)))
	}

	return &ToolResult{
		Content: sb.String(),
	}, nil
}
