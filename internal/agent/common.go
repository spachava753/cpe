package agent

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"github.com/spachava753/gai"
	"strings"
)

// DetectInputType detects the type of input from a file
func DetectInputType(content []byte) (gai.Modality, error) {
	mime := mimetype.Detect(content)
	switch {
	case strings.HasPrefix(mime.String(), "text/"):
		return gai.Text, nil
	case strings.HasPrefix(mime.String(), "image/"), mime.String() == "application/pdf":
		return gai.Image, nil
	case strings.HasPrefix(mime.String(), "video/"):
		return gai.Video, nil
	case strings.HasPrefix(mime.String(), "audio/"):
		return gai.Audio, nil
	default:
		return 0, fmt.Errorf("unsupported file type: %s", mime.String())
	}
}
