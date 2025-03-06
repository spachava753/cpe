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
	Description: `A tool to change the current working directory
* The tool accepts a single parameter "path" specifying the target directory
* Returns the full path of the new directory if successful
* Returns an error message if the directory doesn't exist
* If you need to create, delete, or modify files outside of the current folder, you can use this tool to change the current folder`,
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

// New specialized file operation tools
var createFileTool = Tool{
	Name: "create_file",
	Description: `A tool to create a new file in the current folder or its subfolders.
* 'path' must specify where to create the file (can include subdirectories)
* 'file_text' must be supplied as the contents of the new file
* Will error if the file already exists
* Will create parent directories automatically if they don't exist`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path where the file should be created",
			},
			"file_text": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the new file",
			},
		},
		"required": []string{"path", "file_text"},
	},
}

var deleteFileTool = Tool{
	Name: "delete_file",
	Description: `A tool to delete an existing file in the current folder or its subfolders.
* 'path' must specify the file to delete
* Will error if the file doesn't exist
* Will error if the path is a directory instead of a file`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to the file to delete",
			},
		},
		"required": []string{"path"},
	},
}

var editFileTool = Tool{
	Name: "edit_file",
	Description: `A tool to edit an existing file in the current folder or its subfolders.
* 'path' must specify the file to edit
* 'old_str' should match EXACTLY one or more consecutive lines from the original file, including whitespace
* 'old_str' must be unique in the file (only one match allowed)
* 'new_str' contains the edited text that will replace 'old_str'
* Will error if the file doesn't exist or if old_str isn't found exactly once`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to the file to edit",
			},
			"old_str": map[string]interface{}{
				"type":        "string",
				"description": "The exact text segment to replace (must be unique in the file)",
			},
			"new_str": map[string]interface{}{
				"type":        "string",
				"description": "The new text to replace the old text with",
			},
		},
		"required": []string{"path", "old_str", "new_str"},
	},
}

var moveFileTool = Tool{
	Name: "move_file",
	Description: `A tool to move or rename a file in the current folder or its subfolders.
* 'source_path' specifies the file to move/rename
* 'target_path' specifies the destination file path
* Will error if source file doesn't exist or if target file already exists
* Will create parent directories of target automatically if they don't exist`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"source_path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to the file to move/rename",
			},
			"target_path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path where the file should be moved/renamed to",
			},
		},
		"required": []string{"source_path", "target_path"},
	},
}

// New specialized folder operation tools
var createFolderTool = Tool{
	Name: "create_folder",
	Description: `A tool to create a new folder in the current folder or its subfolders.
* 'path' must specify where to create the folder
* Will error if the folder already exists
* Will create parent directories automatically if they don't exist`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path where the folder should be created",
			},
		},
		"required": []string{"path"},
	},
}

var deleteFolderTool = Tool{
	Name: "delete_folder",
	Description: `A tool to delete an existing folder in the current folder or its subfolders.
* 'path' must specify the folder to delete
* 'recursive' determines whether to delete non-empty folders (defaults to false)
* Will error if the folder doesn't exist
* Will error if the path is a file instead of a folder
* Will error if the folder is not empty and recursive=false`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to the folder to delete",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to delete non-empty folders (true) or error on non-empty folders (false)",
				"default":     false,
			},
		},
		"required": []string{"path"},
	},
}

var moveFolderTool = Tool{
	Name: "move_folder",
	Description: `A tool to move or rename a folder in the current folder or its subfolders.
* 'source_path' specifies the folder to move/rename
* 'target_path' specifies the destination folder path
* Will error if source folder doesn't exist or if target folder already exists
* Will create parent directories of target automatically if they don't exist`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"source_path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to the folder to move/rename",
			},
			"target_path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path where the folder should be moved/renamed to",
			},
		},
		"required": []string{"source_path", "target_path"},
	},
}

// View file tool
var viewFileTool = Tool{
	Name: "view_file",
	Description: `A tool to view the full contents of a file in the current folder or its subfolders.
* 'path' must specify the file to view
* Will error if the file doesn't exist or if path points to a directory
* Will error if the file is binary (non-text)
* Returns the complete contents of the file as a string`,
	InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path to the file to view",
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
