package input

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/urlhandler"
)

const maxInputBytes = 50 * 1024 * 1024

// DetectInputType detects the input modality from raw content bytes.
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

// BuildUserBlocks creates a gai.Block slice from a text prompt and resource paths.
// The prompt is added as the first text block if non-empty. Resource paths can be
// local file paths or URLs.
func BuildUserBlocks(ctx context.Context, prompt string, resourcePaths []string) ([]gai.Block, error) {
	var blocks []gai.Block

	if prompt != "" {
		blocks = append(blocks, gai.Block{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str(prompt),
		})
	}

	for _, resourcePath := range resourcePaths {
		block, err := loadResource(ctx, resourcePath)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

func loadResource(ctx context.Context, resourcePath string) (gai.Block, error) {
	var content []byte
	var filename string
	var contentType string

	if urlhandler.IsURL(resourcePath) {
		downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		downloaded, err := urlhandler.DownloadContent(downloadCtx, resourcePath, nil)
		if err != nil {
			return gai.Block{}, fmt.Errorf("failed to download content from URL %s: %w", resourcePath, err)
		}

		content = downloaded.Data
		filename = filepath.Base(resourcePath)
		contentType = downloaded.ContentType
	} else {
		if _, err := os.Stat(resourcePath); os.IsNotExist(err) {
			return gai.Block{}, fmt.Errorf("input file does not exist: %s", resourcePath)
		}

		var err error
		content, err = os.ReadFile(resourcePath)
		if err != nil {
			return gai.Block{}, fmt.Errorf("failed to read input file %s: %w", resourcePath, err)
		}

		filename = filepath.Base(resourcePath)
	}

	if len(content) > maxInputBytes {
		return gai.Block{}, fmt.Errorf("input content from %s exceeds maximum size limit (50MB)", resourcePath)
	}

	modality, err := DetectInputType(content)
	if err != nil {
		return gai.Block{}, fmt.Errorf("failed to detect input type for %s: %w", resourcePath, err)
	}

	var mime string
	if contentType != "" {
		mime = strings.Split(contentType, ";")[0]
	} else {
		mime = mimetype.Detect(content).String()
	}

	switch modality {
	case gai.Text:
		return gai.Block{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str(content),
		}, nil
	case gai.Video:
		contentStr := base64.StdEncoding.EncodeToString(content)
		return gai.Block{
			BlockType:    gai.Content,
			ModalityType: modality,
			MimeType:     mime,
			Content:      gai.Str(contentStr),
		}, nil
	case gai.Audio:
		return gai.AudioBlock(content, mime), nil
	case gai.Image:
		if mime == "application/pdf" {
			return gai.PDFBlock(content, filename), nil
		}
		return gai.ImageBlock(content, mime), nil
	default:
		return gai.Block{}, fmt.Errorf("unsupported input type for %s", resourcePath)
	}
}
