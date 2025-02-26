package agent

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	ignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/symbolresolver"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// FileInfo represents a file's path and content
type FileInfo struct {
	Path    string
	Content string
}

// ListTextFiles walks through the current directory recursively and returns text files
func ListTextFiles(ignorer *ignore.GitIgnore) ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and ignored files
		if info.IsDir() || ignorer.MatchesPath(path) {
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("error reading file %s: %w", path, err)
		}

		// Detect if file is text
		mime := mimetype.Detect(content)
		if !strings.HasPrefix(mime.String(), "text/") {
			return nil
		}

		files = append(files, FileInfo{
			Path:    path,
			Content: string(content),
		})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %w", err)
	}

	// Sort files by path for consistent output
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

// DetectInputType detects the type of input from a file
func DetectInputType(path string) (InputType, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	mime := mimetype.Detect(content)
	switch {
	case strings.HasPrefix(mime.String(), "text/"):
		return InputTypeText, nil
	case strings.HasPrefix(mime.String(), "image/"):
		return InputTypeImage, nil
	case strings.HasPrefix(mime.String(), "video/"):
		return InputTypeVideo, nil
	case strings.HasPrefix(mime.String(), "audio/"):
		return InputTypeAudio, nil
	default:
		return "", fmt.Errorf("unsupported file type: %s", mime.String())
	}
}

var bashTool = Tool{
	Name: "bash",
	Description: `Run commands in a bash shell
* When invoking this tool, the contents of the "command" parameter does NOT need to be escaped.
* You can access the internet via this tool with CLI's like "curl" or "wget" command.
* You can install the necessary dependencies for your project with this tool, e.g. "pip install", "npm install", "apt-get install", "brew install", etc.
* State EXCEPT environment variables are persisted between calls.
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
	Description: `A tool to edit, create and delete files found in the current folder and any subfolders. Keep in mind that this tool does not allow modifying files outside current folder.
* The "create" command should only be used to create a new file, and "file_text" must be supplied as the contents of the new file. It will error if the file already exists.
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
				"description": `The commands to run. Allowed options are: "create", "str_replace", "remove".`,
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
* The file can be of any type, as long as it contains only text
* You should use this tool to get an understanding of a codebase and to select input files to pass to the 'get_related_files' before attempting to address tasks that require you to understand and/or modify the codebase`,
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
* This tool should only be called after the "files_overview" tool
Note: You may not deem it necessary to call this tool if you have all the information necessary from calling the 'files_overview' tool. However, if you plan to modify the codebase, always call this tool, as it will aid you in getting a better understanding of the files you are about to modify by providing you with the full content of the input files and any relevant files`,
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

var changeDirectoryTool = Tool{
	Name: "change_directory",
	Description: fmt.Sprintf(`A tool to change the current working directory
* The tool accepts a single parameter "path" specifying the target directory
* Returns the full path of the new directory if successful
* Returns an error message if the directory doesn't exist
* If you need to create, delete, or modify files outside of the current folder with the '%s' tool, you can use this tool to change the current folder`, fileEditor.Name),
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to change to, can be relative or absolute",
			},
		},
		"required": []string{"path"},
	},
}

type ToolResult struct {
	ToolUseID string
	Content   any
	IsError   bool
}

// executeBashTool validates and executes the bash tool
func executeBashTool(command string) (*ToolResult, error) {
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
func executeFileEditorTool(params FileEditorParams) (*ToolResult, error) {

	switch params.Command {
	case "create":
		if params.FileText == "" {
			return &ToolResult{
				Content: "file_text parameter is required for create command",
				IsError: true,
			}, nil
		}
		// Check if file already exists before attempting to create it
		if _, err := os.Stat(params.Path); err == nil {
			return &ToolResult{
				Content: fmt.Sprintf("File already exists: %s", params.Path),
				IsError: true,
			}, nil
		} else if !os.IsNotExist(err) {
			// Some other error occurred while checking file existence
			return &ToolResult{
				Content: fmt.Sprintf("Error checking if file exists: %s", err),
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

		count := strings.Count(string(content), params.OldStr)
		if count == 0 {
			return &ToolResult{
				Content: "old_str not found in file",
				IsError: true,
			}, nil
		}
		if count > 1 {
			return &ToolResult{
				Content: fmt.Sprintf("old_str matches %d times in file, expected exactly one match", count),
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

// ExecuteFilesOverviewTool validates and executes the files overview tool
func ExecuteFilesOverviewTool(ignorer *ignore.GitIgnore) (*ToolResult, error) {
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

// ExecuteGetRelatedFilesTool validates and executes the get related files tool
func ExecuteGetRelatedFilesTool(inputFiles []string, ignorer *ignore.GitIgnore) (*ToolResult, error) {

	relatedFiles, err := symbolresolver.ResolveTypeAndFunctionFiles(inputFiles, os.DirFS("."), ignorer)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve related files: %w", err)
	}

	// Convert map to sorted slice for consistent output
	var files []string
	for file := range relatedFiles {
		files = append(files, file)
	}
	sort.Strings(files)

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

// executeChangeDirectoryTool validates and executes the change directory tool
func executeChangeDirectoryTool(path string) (*ToolResult, error) {
	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error resolving absolute path: %s", err),
			IsError: true,
		}, nil
	}

	// Check if directory exists
	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Directory does not exist: %s", absPath),
			IsError: true,
		}, nil
	}

	// Change directory
	if err := os.Chdir(absPath); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error changing directory: %s", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: absPath,
	}, nil
}
