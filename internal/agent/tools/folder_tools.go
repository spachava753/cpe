package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateFolderParams represents the parameters for the create folder tool
type CreateFolderParams struct {
	Path string `json:"path"`
}

// DeleteFolderParams represents the parameters for the delete folder tool
type DeleteFolderParams struct {
	Path    string `json:"path"`
	Recursive bool  `json:"recursive"`
}

// MoveFolderParams represents the parameters for the move folder tool
type MoveFolderParams struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
}

// CreateFolderTool validates and executes the create folder tool
func CreateFolderTool(params CreateFolderParams) (*ToolResult, error) {
	// Check if folder already exists
	if _, err := os.Stat(params.Path); err == nil {
		return &ToolResult{
			Content: fmt.Sprintf("Folder already exists: %s", params.Path),
			IsError: true,
		}, nil
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking folder existence
		return &ToolResult{
			Content: fmt.Sprintf("Error checking if folder exists: %s", err),
			IsError: true,
		}, nil
	}

	// Create the folder with all necessary parent directories
	if err := os.MkdirAll(params.Path, 0755); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error creating folder: %s", err),
			IsError: true,
		}, nil
	}
	return &ToolResult{
		Content: fmt.Sprintf("Successfully created folder %s", params.Path),
	}, nil
}

// DeleteFolderTool validates and executes the delete folder tool
func DeleteFolderTool(params DeleteFolderParams) (*ToolResult, error) {
	// Check if folder exists
	fileInfo, err := os.Stat(params.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("Folder does not exist: %s", params.Path),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error checking folder: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure it's a directory
	if !fileInfo.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Path is a file, not a folder: %s. Use delete_file tool instead.", params.Path),
			IsError: true,
		}, nil
	}

	// Check if the directory is empty if we're not using recursive deletion
	if !params.Recursive {
		entries, err := os.ReadDir(params.Path)
		if err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error reading directory: %s", err),
				IsError: true,
			}, nil
		}
		if len(entries) > 0 {
			return &ToolResult{
				Content: fmt.Sprintf("Folder is not empty: %s. Use recursive=true to delete non-empty folders.", params.Path),
				IsError: true,
			}, nil
		}
		
		// Delete the empty directory
		if err := os.Remove(params.Path); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error removing folder: %s", err),
				IsError: true,
			}, nil
		}
	} else {
		// Delete the directory and all its contents recursively
		if err := os.RemoveAll(params.Path); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error removing folder recursively: %s", err),
				IsError: true,
			}, nil
		}
	}
	
	return &ToolResult{
		Content: fmt.Sprintf("Successfully removed folder %s", params.Path),
	}, nil
}

// MoveFolderTool validates and executes the move folder tool
func MoveFolderTool(params MoveFolderParams) (*ToolResult, error) {
	// Check if source folder exists
	sourceInfo, err := os.Stat(params.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("Source folder does not exist: %s", params.SourcePath),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error checking source folder: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure source is a directory
	if !sourceInfo.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Source path is a file, not a folder: %s. Use move_file tool instead.", params.SourcePath),
			IsError: true,
		}, nil
	}

	// Check if target already exists
	if _, err := os.Stat(params.TargetPath); err == nil {
		return &ToolResult{
			Content: fmt.Sprintf("Target folder already exists: %s", params.TargetPath),
			IsError: true,
		}, nil
	} else if !os.IsNotExist(err) {
		return &ToolResult{
			Content: fmt.Sprintf("Error checking target folder: %s", err),
			IsError: true,
		}, nil
	}

	// Ensure the parent directory of the target exists
	targetParent := filepath.Dir(params.TargetPath)
	if targetParent != "." && targetParent != ".." {
		if err := os.MkdirAll(targetParent, 0755); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Error creating parent directory structure: %s", err),
				IsError: true,
			}, nil
		}
	}

	// Move the folder
	if err := os.Rename(params.SourcePath, params.TargetPath); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error moving folder: %s", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("Successfully moved folder from %s to %s", params.SourcePath, params.TargetPath),
	}, nil
}