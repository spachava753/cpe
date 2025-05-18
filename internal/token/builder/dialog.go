package builder

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/spachava753/gai"
)

// BuildDialog creates a gai.Dialog from content with proper modality detection
func BuildDialog(content []byte) (gai.Dialog, error) {
	// Detect the MIME type of the content
	mime := mimetype.Detect(content)
	mimeStr := mime.String()

	// Choose the appropriate modality based on MIME type
	var modalityType gai.Modality
	var processedContent gai.Str

	if strings.HasPrefix(mimeStr, "text/") ||
		strings.HasPrefix(mimeStr, "application/json") ||
		strings.HasPrefix(mimeStr, "application/xml") ||
		strings.HasPrefix(mimeStr, "application/javascript") {
		// Text modality - use content as string
		modalityType = gai.Text
		processedContent = gai.Str(string(content))
	} else if strings.HasPrefix(mimeStr, "image/") {
		// Image modality - base64 encode the content
		modalityType = gai.Image
		processedContent = gai.Str(base64.StdEncoding.EncodeToString(content))
	} else if strings.HasPrefix(mimeStr, "audio/") {
		// Audio modality - base64 encode the content
		modalityType = gai.Audio
		processedContent = gai.Str(base64.StdEncoding.EncodeToString(content))
	} else if strings.HasPrefix(mimeStr, "video/") {
		// Video modality - base64 encode the content
		modalityType = gai.Video
		processedContent = gai.Str(base64.StdEncoding.EncodeToString(content))
	} else {
		// Unsupported modality
		return nil, fmt.Errorf("unsupported content type: %s", mimeStr)
	}

	// Create a dialog with the content
	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Content,
					ModalityType: modalityType,
					MimeType:     mimeStr,
					Content:      processedContent,
				},
			},
		},
	}

	return dialog, nil
}
