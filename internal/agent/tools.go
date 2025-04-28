package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gabriel-vasile/mimetype"
	ignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/codemap"
	"github.com/spachava753/cpe/internal/symbolresolver"
	"github.com/spachava753/gai"
)

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

type ToolResult struct {
	ToolUseID string
	Content   any
	IsError   bool
}

// executeBashTool validates and executes the bash tool
var outStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "22", Dark: "120"})            // adaptive green
var errStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "52", Dark: "197"}).Bold(true) // adaptive red

type bashToolInput struct {
	Command string `json:"command"`
}

func (b bashToolInput) Validate() error {
	if b.Command == "" {
		return errors.New("command is required")
	}
	return nil
}

func executeBashTool(ctx context.Context, input bashToolInput) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)
	cmd.Env = os.Environ()

	combined, err := cmd.CombinedOutput()
	// Print the combined output EXACTLY as bash would (no color, no splitting)
	if len(combined) > 0 {
		os.Stdout.Write(combined)
	}

	// Print exit code at the end	similar to shell style
	exitCode := 0
	if err != nil {
		// Try to extract the exit code from the error
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(interface{ ExitStatus() int }); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = 1 // fallback if we can't extract
			}
		} else {
			exitCode = 1 // fallback
		}
	}

	if exitCode != 0 {
		fmt.Println(errStyle.Render(fmt.Sprintf("exit code: %d", exitCode)))
		return "", fmt.Errorf("command failed with exit code %d; output:\n%s", exitCode, string(combined))
	}

	fmt.Println(outStyle.Render("exit code: 0"))
	return string(combined), nil
}

// FileEditorParams represents the parameters for the file editor tool
type FileEditorParams struct {
	Command  string `json:"command"`
	Path     string `json:"path"`
	FileText string `json:"file_text,omitempty"`
	OldStr   string `json:"old_str,omitempty"`
	NewStr   string `json:"new_str,omitempty"`
}

type FileOverviewInput struct {
	Path string `json:"path"`
}

func (f FileOverviewInput) Validate() error {
	if f.Path == "" {
		return nil
	}

	// Check if the path exists
	fileInfo, err := os.Stat(f.Path)
	if err != nil {
		return fmt.Errorf("error: the specified path '%s' does not exist or is not accessible", f.Path)
	}

	// Check if the path is a file instead of a directory
	if !fileInfo.IsDir() {
		return fmt.Errorf("error: the specified path '%s' is a file, not a directory. The path should be a relative file path to a folder. If you want to view a single file, you should use the view_file tool instead", f.Path)
	}

	return nil
}

func CreateExecuteFilesOverviewFunc(ignorer *ignore.GitIgnore) gai.ToolCallBackFunc[FileOverviewInput] {
	return func(ctx context.Context, f FileOverviewInput) (string, error) {
		if f.Path == "" {
			f.Path = "."
		}

		// Continue with the directory processing
		fsys := os.DirFS(f.Path)
		files, err := codemap.GenerateOutput(fsys, 100, ignorer)
		if err != nil {
			return "", fmt.Errorf("error: failed to generate code map for '%s': %v", f.Path, err)
		}

		var sb strings.Builder
		for _, file := range files {
			sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file.Path, file.Content))
		}

		return sb.String(), nil
	}
}

type GetRelatedFilesInput struct {
	InputFiles []string `json:"input_files"`
}

func (g GetRelatedFilesInput) Validate() error {
	if len(g.InputFiles) == 0 {
		return errors.New("input_files is required and must not be empty")
	}
	return nil
}

func CreateExecuteGetRelatedFilesFunc(ignorer *ignore.GitIgnore) gai.ToolCallBackFunc[GetRelatedFilesInput] {
	return func(ctx context.Context, input GetRelatedFilesInput) (string, error) {
		// Check all input files exist before continuing.
		var missing []string
		for _, file := range input.InputFiles {
			if _, err := os.Stat(file); err != nil {
				missing = append(missing, file)
			}
		}
		if len(missing) > 0 {
			return "", fmt.Errorf("the following input files do not exist or are not accessible: %s", strings.Join(missing, ", "))
		}

		relatedFiles, err := symbolresolver.ResolveTypeAndFunctionFiles(input.InputFiles, os.DirFS("."), ignorer)
		if err != nil {
			return "", fmt.Errorf("failed to resolve related files: %v", err)
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
				return "", fmt.Errorf("failed to read file %s: %v", file, err)
			}
			sb.WriteString(fmt.Sprintf("File: %s\nContent:\n```%s```\n\n", file, string(content)))
		}

		return sb.String(), nil
	}
}

var bashTool = gai.Tool{
	Name: "bash",
	Description: `Run commands in a bash shell
* When invoking this tool, the contents of the "command" parameter does NOT need to be escaped.
* You can access the internet via this tool with CLI's like "curl" or "wget" command.
* You can install the necessary dependencies for your project with this tool, e.g. "pip install", "npm install", "apt-get install", "brew install", etc.
* State EXCEPT environment variables are persisted between calls.
* To inspect a particular line range of a file, e.g. lines 10-25, try 'sed -n 10,25p /path/to/the/file'.
* Avoid commands that may produce a very large amount of output.
* Run long lived commands in the background, e.g. 'sleep 10 &' or start a server in the background`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"command": {
				Type:        gai.String,
				Description: "The bash command to run.",
			},
		},
		Required: []string{"command"},
	},
}

var filesOverviewTool = gai.Tool{
	Name: "files_overview",
	Description: `A tool to get an overview of all of the files found recursively in the current directory or a subfolder.
* Each file is recursively listed with its relative path from the provided directory (or the current directory if not specified) and the contents of the file.
* The contents of the file may omit certain lines to reduce the number of lines returned. For example, for source code files, the function and method bodies are omitted.
* The file can be of any type, as long as it contains only text
* You should use this tool to get an understanding of a codebase or subfolder and to select input files to pass to the 'get_related_files' before attempting to address tasks that require you to understand and/or modify the codebase`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Optional path to the folder to overview. Defaults to '.' (current directory) if not provided.",
			},
		},
	},
}

var getRelatedFilesTool = gai.Tool{
	Name: "get_related_files",
	Description: `A tool to help retrieve relevant files for a given set of input files
* If the input files contain source code files, symbols like functions and types are extracted and matched in other files that contain the symbol's definition
* If the input files contain other files, the tool will try to find files that mention the input files' names
* This tool should only be called after the "files_overview" tool
Note: You may not deem it necessary to call this tool if you have all the information necessary from calling the 'files_overview' tool. However, if you plan to modify the codebase, always call this tool, as it will aid you in getting a better understanding of the files you are about to modify by providing you with the full content of the input files and any relevant files`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"input_files": {
				Type:        gai.Array,
				Description: `An array of input files to retrieve related files, e.g. source code files that have symbol definitions in another file or other files that mention the file's name.'`,
				Items: &gai.Property{
					Type: gai.String,
				},
			},
		},
		Required: []string{"input_files"},
	},
}

// File operation tools
var createFileTool = gai.Tool{
	Name: "create_file",
	Description: `A tool to create a new file in the current folder or its subfolders.
* 'path' must specify where to create the file (can include subdirectories)
* 'file_text' must be supplied as the contents of the new file
* Will error if the file already exists
* Will create parent directories automatically if they don't exist`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Relative path where the file should be created",
			},
			"file_text": {
				Type:        gai.String,
				Description: "Content to write to the new file",
			},
		},
		Required: []string{"path", "file_text"},
	},
}

var deleteFileTool = gai.Tool{
	Name: "delete_file",
	Description: `A tool to delete an existing file in the current folder or its subfolders.
* 'path' must specify the file to delete
* Will error if the file doesn't exist
* Will error if the path is a directory instead of a file`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Relative path to the file to delete",
			},
		},
		Required: []string{"path"},
	},
}

var editFileTool = gai.Tool{
	Name: "edit_file",
	Description: `A tool to edit, delete, or append content to an existing file in the current folder or its subfolders.
* 'path' must specify the file to edit
* If both 'old_str' and 'new_str' are provided, replaces the unique occurrence of 'old_str' with 'new_str' (edit).
* If 'old_str' is provided and 'new_str' is missing or blank, deletes the unique occurrence of 'old_str' from the file (delete).
* If 'new_str' is provided and 'old_str' is missing or blank, appends 'new_str' to the end of the file (append).
* If neither are provided, the operation errors.
* For edit or delete modes, 'old_str' must match exactly one unique occurrence in the file (including whitespace).
* Errors if file does not exist, or match count conditions are not met.`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Relative path to the file to edit",
			},
			"old_str": {
				Type:        gai.String,
				Description: "The exact text segment to search for replacement or deletion (must be unique if provided)",
			},
			"new_str": {
				Type:        gai.String,
				Description: "The new text to replace the old text with, or text to append (if old_str missing)",
			},
		},
		Required: []string{"path"},
	},
}

var moveFileTool = gai.Tool{
	Name: "move_file",
	Description: `A tool to move or rename a file in the current folder or its subfolders.
* 'source_path' specifies the file to move/rename
* 'target_path' specifies the destination file path
* Will error if source file doesn't exist or if target file already exists
* Will create parent directories of target automatically if they don't exist`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"source_path": {
				Type:        gai.String,
				Description: "Relative path to the file to move/rename",
			},
			"target_path": {
				Type:        gai.String,
				Description: "Relative path where the file should be moved/renamed to",
			},
		},
		Required: []string{"source_path", "target_path"},
	},
}

var viewFileTool = gai.Tool{
	Name: "view_file",
	Description: `A tool to view the full contents of a file in the current folder or its subfolders.
* 'path' must specify the file to view
* Will error if the file doesn't exist or if path points to a directory
* Will error if the file is binary (non-text)
* Returns the complete contents of the file as a string`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Relative path to the file to view",
			},
		},
		Required: []string{"path"},
	},
}

// Folder operation tools
var createFolderTool = gai.Tool{
	Name: "create_folder",
	Description: `A tool to create a new folder in the current folder or its subfolders.
* 'path' must specify where to create the folder
* Will error if the folder already exists
* Will create parent directories automatically if they don't exist`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Relative path where the folder should be created",
			},
		},
		Required: []string{"path"},
	},
}

var deleteFolderTool = gai.Tool{
	Name: "delete_folder",
	Description: `A tool to delete an existing folder in the current folder or its subfolders.
* 'path' must specify the folder to delete
* 'recursive' determines whether to delete non-empty folders (defaults to false)
* Will error if the folder doesn't exist
* Will error if the path is a file instead of a folder
* Will error if the folder is not empty and recursive=false`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"path": {
				Type:        gai.String,
				Description: "Relative path to the folder to delete",
			},
			"recursive": {
				Type:        gai.Boolean,
				Description: "Whether to delete non-empty folders (true) or error on non-empty folders (false)",
			},
		},
		Required: []string{"path"},
	},
}

var moveFolderTool = gai.Tool{
	Name: "move_folder",
	Description: `A tool to move or rename a folder in the current folder or its subfolders.
* 'source_path' specifies the folder to move/rename
* 'target_path' specifies the destination folder path
* Will error if source folder doesn't exist or if target folder already exists
* Will create parent directories of target automatically if they don't exist`,
	InputSchema: gai.InputSchema{
		Type: gai.Object,
		Properties: map[string]gai.Property{
			"source_path": {
				Type:        gai.String,
				Description: "Relative path to the folder to move/rename",
			},
			"target_path": {
				Type:        gai.String,
				Description: "Relative path where the folder should be moved/renamed to",
			},
		},
		Required: []string{"source_path", "target_path"},
	},
}
