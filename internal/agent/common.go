package agent

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"github.com/spachava753/gai"
	"os"
	"strings"
)

// DetectInputType detects the type of input from a file
func DetectInputType(path string) (gai.Modality, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("error reading file: %w", err)
	}

	mime := mimetype.Detect(content)
	switch {
	case strings.HasPrefix(mime.String(), "text/"):
		return gai.Text, nil
	case strings.HasPrefix(mime.String(), "image/"):
		return gai.Image, nil
	case strings.HasPrefix(mime.String(), "video/"):
		return gai.Video, nil
	case strings.HasPrefix(mime.String(), "audio/"):
		return gai.Audio, nil
	default:
		return 0, fmt.Errorf("unsupported file type: %s", mime.String())
	}
}
