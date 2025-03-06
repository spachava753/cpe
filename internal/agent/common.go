package agent

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"os"
	"strings"
)

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
