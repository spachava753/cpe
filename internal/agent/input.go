package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/spachava753/cpe/internal/urlhandler"
	"github.com/spachava753/gai"
)

// BuildUserBlocks creates gai.Block slice from a text prompt and resource paths.
// The prompt is added as the first text block if non-empty.
// Resource paths can be local file paths or URLs.
func BuildUserBlocks(ctx context.Context, prompt string, resourcePaths []string) ([]gai.Block, error) {
	var blocks []gai.Block

	// Add the prompt as a text block if provided
	if prompt != "" {
		blocks = append(blocks, gai.Block{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str(prompt),
		})
	}

	// Process each resource path (file or URL)
	for _, resourcePath := range resourcePaths {
		block, err := loadResource(ctx, resourcePath)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

// loadResource loads a resource from a file path or URL and returns it as a gai.Block
func loadResource(ctx context.Context, resourcePath string) (gai.Block, error) {
	var content []byte
	var filename string
	var contentType string

	// Check if input is a URL or file path
	if urlhandler.IsURL(resourcePath) {
		// Handle URL input
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
		// Handle local file input
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

	// Apply size limits to prevent memory issues
	if len(content) > 50*1024*1024 { // 50MB limit
		return gai.Block{}, fmt.Errorf("input content from %s exceeds maximum size limit (50MB)", resourcePath)
	}

	// Detect input type (text, image, etc.)
	modality, err := DetectInputType(content)
	if err != nil {
		return gai.Block{}, fmt.Errorf("failed to detect input type for %s: %w", resourcePath, err)
	}

	// Determine MIME type
	var mime string
	if contentType != "" {
		// Use Content-Type from HTTP response if available
		mime = strings.Split(contentType, ";")[0] // Remove charset and other parameters
	} else {
		// Fall back to content-based detection
		mime = mimetype.Detect(content).String()
	}

	// Create block based on modality
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
