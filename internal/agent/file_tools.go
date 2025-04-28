package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// CreateFileInput represents the parameters for the create file tool
type CreateFileInput struct {
	Path     string `json:"path"`
	FileText string `json:"file_text"`
}

func (c CreateFileInput) Validate() error {
	if c.Path == "" {
		return errors.New("path is required")
	}
	if c.FileText == "" {
		return errors.New("file_text is required")
	}

	// For create operations, we should verify the file doesn't already exist
	if _, err := os.Stat(c.Path); err == nil {
		return fmt.Errorf("file already exists at path: %s", c.Path)
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking
		return fmt.Errorf("error checking path: %s", err)
	}

	return nil
}

// DeleteFileInput represents the parameters for the delete file tool
type DeleteFileInput struct {
	Path string `json:"path"`
}

func (d DeleteFileInput) Validate() error {
	if d.Path == "" {
		return errors.New("path is required")
	}

	// Verify the path exists and is a file
	fileInfo, err := os.Stat(d.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist at path: %s", d.Path)
		}
		return fmt.Errorf("error checking path: %s", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", d.Path)
	}

	return nil
}

// EditFileInput represents the parameters for the edit file tool
type EditFileInput struct {
	Path   string `json:"path"`
	OldStr string `json:"old_str"`
	NewStr string `json:"new_str"`
}

func (e EditFileInput) Validate() error {
	if e.Path == "" {
		return errors.New("path is required")
	}
	if e.OldStr == "" && e.NewStr == "" {
		return errors.New("at least one of old_str or new_str must be provided")
	}

	// Verify the path exists and is a file
	fileInfo, err := os.Stat(e.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist at path: %s", e.Path)
		}
		return fmt.Errorf("error checking path: %s", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", e.Path)
	}

	return nil
}

// MoveFileInput represents the parameters for the move file tool
type MoveFileInput struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
}

func (m MoveFileInput) Validate() error {
	if m.SourcePath == "" {
		return errors.New("source_path is required")
	}
	if m.TargetPath == "" {
		return errors.New("target_path is required")
	}

	// Verify the source path exists and is a file
	sourceInfo, err := os.Stat(m.SourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source file does not exist: %s", m.SourcePath)
		}
		return fmt.Errorf("error checking source path: %s", err)
	}

	if sourceInfo.IsDir() {
		return fmt.Errorf("source path is a directory, not a file: %s", m.SourcePath)
	}

	// For target path, we should verify it doesn't already exist
	if _, err := os.Stat(m.TargetPath); err == nil {
		return fmt.Errorf("target file already exists: %s", m.TargetPath)
	} else if !os.IsNotExist(err) {
		// Some other error occurred while checking
		return fmt.Errorf("error checking target path: %s", err)
	}

	return nil
}

// ViewFileInput represents the parameters for the view file tool
type ViewFileInput struct {
	Path string `json:"path"`
}

func (v ViewFileInput) Validate() error {
	if v.Path == "" {
		return errors.New("path is required")
	}

	// Verify the path exists and is a file
	fileInfo, err := os.Stat(v.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist at path: %s", v.Path)
		}
		return fmt.Errorf("error checking path: %s", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", v.Path)
	}

	return nil
}

// ExecuteCreateFile handles creating a file
func ExecuteCreateFile(ctx context.Context, input CreateFileInput) (string, error) {
	// Ensure the directory exists
	dir := filepath.Dir(input.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("error creating directory structure: %s", err)
	}

	if err := os.WriteFile(input.Path, []byte(input.FileText), 0644); err != nil {
		return "", fmt.Errorf("error creating file: %s", err)
	}

	return fmt.Sprintf("Successfully created file %s", input.Path), nil
}

// ExecuteDeleteFile handles deleting a file
func ExecuteDeleteFile(ctx context.Context, input DeleteFileInput) (string, error) {
	if err := os.Remove(input.Path); err != nil {
		return "", fmt.Errorf("error removing file: %s", err)
	}

	return fmt.Sprintf("Successfully removed file %s", input.Path), nil
}

// ExecuteEditFile handles editing a file
func ExecuteEditFile(ctx context.Context, input EditFileInput) (string, error) {
	content, err := os.ReadFile(input.Path)
	if err != nil {
		return "", fmt.Errorf("error reading file: %s", err)
	}

	s := string(content)

	switch {
	// Edit: replace old_str with new_str (both required)
	case input.OldStr != "" && input.NewStr != "":
		count := strings.Count(s, input.OldStr)
		if count == 0 {
			return "", fmt.Errorf("old_str not found in file")
		}
		if count > 1 {
			return "", fmt.Errorf("old_str matches %d times in file, expected exactly one match", count)
		}
		newContent := strings.Replace(s, input.OldStr, input.NewStr, 1)
		if err := os.WriteFile(input.Path, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("error writing file: %s", err)
		}
		return fmt.Sprintf("Successfully edited text in %s", input.Path), nil

	// Delete: only old_str provided
	case input.OldStr != "" && input.NewStr == "":
		count := strings.Count(s, input.OldStr)
		if count == 0 {
			return "", fmt.Errorf("old_str not found in file for deletion")
		}
		if count > 1 {
			return "", fmt.Errorf("old_str matches %d times in file, expected exactly one match for deletion", count)
		}
		newContent := strings.Replace(s, input.OldStr, "", 1)
		if err := os.WriteFile(input.Path, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("error writing file during deletion: %s", err)
		}
		return fmt.Sprintf("Successfully deleted text in %s", input.Path), nil

	// Append: only new_str provided
	case input.OldStr == "" && input.NewStr != "":
		f, err := os.OpenFile(input.Path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return "", fmt.Errorf("error opening file for append: %s", err)
		}
		defer f.Close()
		if _, err := f.WriteString(input.NewStr); err != nil {
			return "", fmt.Errorf("error appending to file: %s", err)
		}
		return fmt.Sprintf("Successfully appended text to %s", input.Path), nil

	// Neither provided - this shouldn't happen because of Validate()
	default:
		return "", fmt.Errorf("must provide at least one of old_str or new_str. See tool description for valid usages")
	}
}

// ExecuteMoveFile handles moving/renaming a file
func ExecuteMoveFile(ctx context.Context, input MoveFileInput) (string, error) {
	// Ensure the target directory exists
	targetDir := filepath.Dir(input.TargetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("error creating target directory structure: %s", err)
	}

	// Move the file
	if err := os.Rename(input.SourcePath, input.TargetPath); err != nil {
		return "", fmt.Errorf("error moving file: %s", err)
	}

	return fmt.Sprintf("Successfully moved file from %s to %s", input.SourcePath, input.TargetPath), nil
}

// ExecuteViewFile handles viewing a file
func ExecuteViewFile(ctx context.Context, input ViewFileInput) (string, error) {
	// Read the file content
	content, err := os.ReadFile(input.Path)
	if err != nil {
		return "", fmt.Errorf("error reading file: %s", err)
	}

	// Detect if file is binary
	mime := mimetype.Detect(content)
	if !strings.HasPrefix(mime.String(), "text/") {
		return "", fmt.Errorf("file appears to be binary (MIME type: %s), not displaying content", mime.String())
	}

	return string(content), nil
}
