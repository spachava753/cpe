package tools

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	ignore "github.com/sabhiram/go-gitignore"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

// InputType defines the type of input for a model
type InputType string

const (
	InputTypeText  InputType = "text"
	InputTypeImage InputType = "image"
	InputTypeVideo InputType = "video"
	InputTypeAudio InputType = "audio"
)

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