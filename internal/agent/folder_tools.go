package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CreateFolderInput represents the parameters for the create folder tool
type CreateFolderInput struct {
	Path string `json:"path"`
}

func (c CreateFolderInput) Validate() error {
	if c.Path == "" {
		return errors.New("path is required")
	}
	return nil
}

// DeleteFolderInput represents the parameters for the delete folder tool
type DeleteFolderInput struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func (d DeleteFolderInput) Validate() error {
	if d.Path == "" {
		return errors.New("path is required")
	}
	return nil
}

// MoveFolderInput represents the parameters for the move folder tool
type MoveFolderInput struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
}

func (m MoveFolderInput) Validate() error {
	if m.SourcePath == "" {
		return errors.New("source_path is required")
	}
	if m.TargetPath == "" {
		return errors.New("target_path is required")
	}
	return nil
}

// ExecuteCreateFolder handles creating a folder
func ExecuteCreateFolder(ctx context.Context, input CreateFolderInput) (string, error) {
	// Check if folder already exists
	if _, err := os.Stat(input.Path); err == nil {
		return "", fmt.Errorf("folder already exists: %s", input.Path)
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking folder existence
		return "", fmt.Errorf("error checking if folder exists: %s", err)
	}

	// Create the folder with all necessary parent directories
	if err := os.MkdirAll(input.Path, 0755); err != nil {
		return "", fmt.Errorf("error creating folder: %s", err)
	}
	return fmt.Sprintf("Successfully created folder %s", input.Path), nil
}

// ExecuteDeleteFolder handles deleting a folder
func ExecuteDeleteFolder(ctx context.Context, input DeleteFolderInput) (string, error) {
	// Check if folder exists
	fileInfo, err := os.Stat(input.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("folder does not exist: %s", input.Path)
		}
		return "", fmt.Errorf("error checking folder: %s", err)
	}

	// Ensure it's a directory
	if !fileInfo.IsDir() {
		return "", fmt.Errorf("path is a file, not a folder: %s. Use delete_file tool instead", input.Path)
	}

	// Check if the directory is empty if we're not using recursive deletion
	if !input.Recursive {
		entries, err := os.ReadDir(input.Path)
		if err != nil {
			return "", fmt.Errorf("error reading directory: %s", err)
		}
		if len(entries) > 0 {
			return "", fmt.Errorf("folder is not empty: %s. Use recursive=true to delete non-empty folders", input.Path)
		}

		// Delete the empty directory
		if err := os.Remove(input.Path); err != nil {
			return "", fmt.Errorf("error removing folder: %s", err)
		}
	} else {
		// Delete the directory and all its contents recursively
		if err := os.RemoveAll(input.Path); err != nil {
			return "", fmt.Errorf("error removing folder recursively: %s", err)
		}
	}

	return fmt.Sprintf("Successfully removed folder %s", input.Path), nil
}

// ExecuteMoveFolder handles moving/renaming a folder
func ExecuteMoveFolder(ctx context.Context, input MoveFolderInput) (string, error) {
	// Check if source folder exists
	sourceInfo, err := os.Stat(input.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("source folder does not exist: %s", input.SourcePath)
		}
		return "", fmt.Errorf("error checking source folder: %s", err)
	}

	// Ensure source is a directory
	if !sourceInfo.IsDir() {
		return "", fmt.Errorf("source path is a file, not a folder: %s. Use move_file tool instead", input.SourcePath)
	}

	// Check if target already exists
	if _, err := os.Stat(input.TargetPath); err == nil {
		return "", fmt.Errorf("target folder already exists: %s", input.TargetPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("error checking target folder: %s", err)
	}

	// Ensure the parent directory of the target exists
	targetParent := filepath.Dir(input.TargetPath)
	if targetParent != "." && targetParent != ".." {
		if err := os.MkdirAll(targetParent, 0755); err != nil {
			return "", fmt.Errorf("error creating parent directory structure: %s", err)
		}
	}

	// Move the folder
	if err := os.Rename(input.SourcePath, input.TargetPath); err != nil {
		return "", fmt.Errorf("error moving folder: %s", err)
	}

	return fmt.Sprintf("Successfully moved folder from %s to %s", input.SourcePath, input.TargetPath), nil
}
