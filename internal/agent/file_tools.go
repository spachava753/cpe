package agent

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"os"
	"path/filepath"
	"strings"
)

// CreateFileParams represents the parameters for the create file tool
type CreateFileParams struct {
	Path     string `json:"path"`
	FileText string `json:"file_text"`
}

// DeleteFileParams represents the parameters for the delete file tool
type DeleteFileParams struct {
	Path string `json:"path"`
}

// EditFileParams represents the parameters for the edit file tool
type EditFileParams struct {
	Path   string `json:"path"`
	OldStr string `json:"old_str"`
	NewStr string `json:"new_str"`
}

// MoveFileParams represents the parameters for the move file tool
type MoveFileParams struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
}

// ViewFileParams represents the parameters for the view file tool
type ViewFileParams struct {
	Path string `json:"path"`
}

// CreateFileTool validates and executes the create file tool
func CreateFileTool(params CreateFileParams) (*ToolResult, error) {
	if params.FileText == "" {
		return &ToolResult{
			Content: "file_text parameter is required for create_file command",
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

	// Ensure the directory exists
	dir := filepath.Dir(params.Path)
	if dir != "." && dir != ".." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error creating directory structure: %s", err),
				IsError: true,
			}, nil
		}
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
}

// DeleteFileTool validates and executes the delete file tool
func DeleteFileTool(params DeleteFileParams) (*ToolResult, error) {
	// Check if file exists
	fileInfo, err := os.Stat(params.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("File does not exist: %s", params.Path),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error checking file: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure it's not a directory
	if fileInfo.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Path is a directory, not a file: %s. Use delete_folder tool instead.", params.Path),
			IsError: true,
		}, nil
	}

	if err := os.Remove(params.Path); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error removing file: %s", err),
			IsError: true,
		}, nil
	}
	return &ToolResult{
		Content: fmt.Sprintf("Successfully removed file %s", params.Path),
	}, nil
}

// EditFileTool validates and executes the edit file tool
func EditFileTool(params EditFileParams) (*ToolResult, error) {
	// Check if file exists
	if _, err := os.Stat(params.Path); err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("File does not exist: %s", params.Path),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error checking file: %s", err),
			IsError: true,
		}, nil
	}

	content, err := os.ReadFile(params.Path)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading file: %s", err),
			IsError: true,
		}, nil
	}

	s := string(content)

	switch {
	// Edit: replace old_str with new_str (both required)
	case params.OldStr != "" && params.NewStr != "":
		count := strings.Count(s, params.OldStr)
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
		newContent := strings.Replace(s, params.OldStr, params.NewStr, 1)
		if err := os.WriteFile(params.Path, []byte(newContent), 0644); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error writing file: %s", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{Content: fmt.Sprintf("Successfully edited text in %s", params.Path)}, nil
	// Delete: only old_str provided
	case params.OldStr != "" && params.NewStr == "":
		count := strings.Count(s, params.OldStr)
		if count == 0 {
			return &ToolResult{
				Content: "old_str not found in file for deletion",
				IsError: true,
			}, nil
		}
		if count > 1 {
			return &ToolResult{
				Content: fmt.Sprintf("old_str matches %d times in file, expected exactly one match for deletion", count),
				IsError: true,
			}, nil
		}
		newContent := strings.Replace(s, params.OldStr, "", 1)
		if err := os.WriteFile(params.Path, []byte(newContent), 0644); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error writing file during deletion: %s", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{Content: fmt.Sprintf("Successfully deleted text in %s", params.Path)}, nil
	// Append: only new_str provided
	case params.OldStr == "" && params.NewStr != "":
		f, err := os.OpenFile(params.Path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error opening file for append: %s", err),
				IsError: true,
			}, nil
		}
		defer f.Close()
		if _, err := f.WriteString(params.NewStr); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error appending to file: %s", err),
				IsError: true,
			}, nil
		}
		return &ToolResult{Content: fmt.Sprintf("Successfully appended text to %s", params.Path)}, nil
	// Neither provided
	default:
		return &ToolResult{
			Content: "Must provide at least one of old_str or new_str. See tool description for valid usages.",
			IsError: true,
		}, nil
	}
}

// MoveFileTool validates and executes the move file tool
func MoveFileTool(params MoveFileParams) (*ToolResult, error) {
	// Check if source file exists
	sourceInfo, err := os.Stat(params.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("Source file does not exist: %s", params.SourcePath),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error checking source file: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure source is not a directory
	if sourceInfo.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Source path is a directory, not a file: %s. Use move_folder tool instead.", params.SourcePath),
			IsError: true,
		}, nil
	}

	// Check if target already exists
	if _, err := os.Stat(params.TargetPath); err == nil {
		return &ToolResult{
			Content: fmt.Sprintf("Target file already exists: %s", params.TargetPath),
			IsError: true,
		}, nil
	} else if !os.IsNotExist(err) {
		return &ToolResult{
			Content: fmt.Sprintf("Error checking target file: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure the target directory exists
	targetDir := filepath.Dir(params.TargetPath)
	if targetDir != "." && targetDir != ".." {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error creating target directory structure: %s", err),
				IsError: true,
			}, nil
		}
	}

	// Move the file
	if err := os.Rename(params.SourcePath, params.TargetPath); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error moving file: %s", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("Successfully moved file from %s to %s", params.SourcePath, params.TargetPath),
	}, nil
}

// ViewFileTool validates and executes the view file tool
func ViewFileTool(params ViewFileParams) (*ToolResult, error) {
	// Check if file exists
	fileInfo, err := os.Stat(params.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("File does not exist: %s", params.Path),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error checking file: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure it's not a directory
	if fileInfo.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Path is a directory, not a file: %s", params.Path),
			IsError: true,
		}, nil
	}

	// Read the file content
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading file: %s", err),
			IsError: true,
		}, nil
	}

	// Detect if file is binary
	mime := mimetype.Detect(content)
	if !strings.HasPrefix(mime.String(), "text/") {
		return &ToolResult{
			Content: fmt.Sprintf("File appears to be binary (MIME type: %s), not displaying content", mime.String()),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: string(content),
	}, nil
}
