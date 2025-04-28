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

	// For create operations, we should verify the folder doesn't already exist
	if _, err := os.Stat(c.Path); err == nil {
		return fmt.Errorf("folder already exists at path: %s", c.Path)
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking
		return fmt.Errorf("error checking path: %s", err)
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

	// Verify the path exists and is a folder
	fileInfo, err := os.Stat(d.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("folder does not exist at path: %s", d.Path)
		}
		return fmt.Errorf("error checking path: %s", err)
	}

	if !fileInfo.IsDir() {
		return fmt.Errorf("path is a file, not a folder: %s", d.Path)
	}

	// If not recursive, verify the folder is empty
	if !d.Recursive {
		entries, err := os.ReadDir(d.Path)
		if err != nil {
			return fmt.Errorf("error reading directory: %s", err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("folder is not empty: %s (use recursive=true to delete non-empty folders)", d.Path)
		}
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

	// Verify the source path exists and is a folder
	sourceInfo, err := os.Stat(m.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source folder does not exist: %s", m.SourcePath)
		}
		return fmt.Errorf("error checking source path: %s", err)
	}

	if !sourceInfo.IsDir() {
		return fmt.Errorf("source path is a file, not a folder: %s", m.SourcePath)
	}

	// For target path, we should verify it doesn't already exist
	if _, err := os.Stat(m.TargetPath); err == nil {
		return fmt.Errorf("target folder already exists: %s", m.TargetPath)
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking
		return fmt.Errorf("error checking target path: %s", err)
	}

	return nil
}

// ExecuteCreateFolder handles creating a folder
func ExecuteCreateFolder(ctx context.Context, input CreateFolderInput) (string, error) {
	// Create the folder with all necessary parent directories
	if err := os.MkdirAll(input.Path, 0755); err != nil {
		return "", fmt.Errorf("error creating folder: %s", err)
	}
	return fmt.Sprintf("Successfully created folder %s", input.Path), nil
}

// ExecuteDeleteFolder handles deleting a folder
func ExecuteDeleteFolder(ctx context.Context, input DeleteFolderInput) (string, error) {
	if !input.Recursive {
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
	// Ensure the parent directory of the target exists
	targetParent := filepath.Dir(input.TargetPath)
	if err := os.MkdirAll(targetParent, 0755); err != nil {
		return "", fmt.Errorf("error creating parent directory structure: %s", err)
	}

	// Move the folder
	if err := os.Rename(input.SourcePath, input.TargetPath); err != nil {
		return "", fmt.Errorf("error moving folder: %s", err)
	}

	return fmt.Sprintf("Successfully moved folder from %s to %s", input.SourcePath, input.TargetPath), nil
}
