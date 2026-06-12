package acp

import (
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"
)

func (a *Agent) promptToMessage(contentBlocks []acp.ContentBlock) gai.Message {
	msg := gai.Message{
		Role:   gai.User,
		Blocks: make([]gai.Block, 0, len(contentBlocks)),
	}
	for _, contentBlock := range contentBlocks {
		var block gai.Block
		switch {
		case contentBlock.Text != nil:
			block = gai.TextBlock(contentBlock.Text.Text)
		case contentBlock.Image != nil:
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Image,
				MimeType:     contentBlock.Image.MimeType,
				// content comes as base64 encoded data
				Content: gai.Str(contentBlock.Image.Data),
			}
		case contentBlock.Audio != nil:
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Audio,
				MimeType:     contentBlock.Audio.MimeType,
				// content comes as base64 encoded data
				Content: gai.Str(contentBlock.Audio.Data),
			}
		case contentBlock.ResourceLink != nil: // TODO: support resource links better
			block = gai.TextBlock(fmt.Sprintf("Resource %s: %s", contentBlock.ResourceLink.Name, contentBlock.ResourceLink.Uri))
		case contentBlock.Resource != nil:
			// embedded context resources can be text or blobs
			// TODO: should limit based on size of embedded resources?
			resource := contentBlock.Resource.Resource
			switch {
			case resource.TextResourceContents != nil:
				var sb strings.Builder
				if _, err := fmt.Fprintf(&sb, "`%s` contents:\n```", resource.TextResourceContents.Uri); err != nil {
					panic(err)
				}
				if _, err := sb.WriteString(resource.TextResourceContents.Text); err != nil {
					panic(err)
				}
				if _, err := sb.WriteString("\n```\n"); err != nil {
					panic(err)
				}
				block = gai.TextBlock(sb.String())
			case resource.BlobResourceContents != nil:
				resourcePath := resource.BlobResourceContents.Uri
				parsed, err := url.Parse(resource.BlobResourceContents.Uri)
				if err != nil {
					slog.Error("failed to parse embedded resource URI", "uri", resource.BlobResourceContents.Uri, "error", err)
					panic(err)
				}
				if parsed.Path != "" {
					resourcePath = parsed.Path
				}
				mt := ""
				if resource.BlobResourceContents.MimeType != nil {
					mt = *resource.BlobResourceContents.MimeType
				}
				if mt == "" {
					mt = mime.TypeByExtension(filepath.Ext(resourcePath))
				}
				if mt == "" {
					msg := fmt.Sprintf("could not detect MIME type for embedded resource URI %q", resource.BlobResourceContents.Uri)
					slog.Error(msg, "uri", resource.BlobResourceContents.Uri, "path", resourcePath)
					panic(msg)
				}
				filename := filepath.Base(resourcePath)
				if filename == "." || filename == string(filepath.Separator) {
					filename = ""
				}
				block = gai.Block{
					BlockType: gai.Content,
					MimeType:  mt,
					// content comes as base64 encoded data
					Content: gai.Str(resource.BlobResourceContents.Blob),
					ExtraFields: map[string]any{
						gai.BlockFieldFilenameKey: filename,
					},
				}
				switch {
				case strings.HasPrefix(mt, "image/"):
					block.ModalityType = gai.Image
				case strings.HasPrefix(mt, "application/pdf"):
					block.ModalityType = gai.Image
				case strings.HasPrefix(mt, "audio/"):
					block.ModalityType = gai.Audio
				case strings.HasPrefix(mt, "video/"):
					block.ModalityType = gai.Video
				}
			}
		}
		msg.Blocks = append(msg.Blocks, block)
	}
	return msg
}

func promptTurnDialog(dialog gai.Dialog, inputLen int) gai.Dialog {
	// Compaction can replace the input history with a shorter rebased dialog.
	// In that case, the returned dialog is the only safe turn boundary we have.
	if inputLen < 0 || inputLen > len(dialog) {
		return dialog
	}
	return dialog[inputLen:]
}
