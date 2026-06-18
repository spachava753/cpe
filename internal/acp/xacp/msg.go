package xacp

import (
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
)

func PromptToMessage(contentBlocks []acp.ContentBlock) gai.Message {
	msg := gai.Message{
		Role:   gai.User,
		Blocks: make([]gai.Block, 0, len(contentBlocks)),
	}
	for _, contentBlock := range contentBlocks {
		var block gai.Block
		switch contentBlock.Type {
		case acp.ContentBlockTypeText:
			block = gai.TextBlock(contentBlock.Text)
		case acp.ContentBlockTypeImage:
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Image,
				MimeType:     stringValue(contentBlock.MimeType),
				// content comes as base64 encoded data
				Content: gai.Str(contentBlock.Data),
			}
		case acp.ContentBlockTypeAudio:
			block = gai.Block{
				BlockType:    gai.Content,
				ModalityType: gai.Audio,
				MimeType:     stringValue(contentBlock.MimeType),
				// content comes as base64 encoded data
				Content: gai.Str(contentBlock.Data),
			}
		case acp.ContentBlockTypeResourceLink: // TODO: support resource links better
			block = gai.TextBlock(fmt.Sprintf("Resource %s: %s", contentBlock.Name, stringValue(contentBlock.URI)))
		case acp.ContentBlockTypeResource:
			// embedded context resources can be text or blobs
			// TODO: should limit based on size of embedded resources?
			resource := contentBlock.Resource
			if resource.Blob == "" {
				var sb strings.Builder
				if _, err := fmt.Fprintf(&sb, "`%s` contents:\n```", resource.URI); err != nil {
					panic(err)
				}
				if _, err := sb.WriteString(resource.Text); err != nil {
					panic(err)
				}
				if _, err := sb.WriteString("\n```\n"); err != nil {
					panic(err)
				}
				block = gai.TextBlock(sb.String())
			} else {
				resourcePath := resource.URI
				parsed, err := url.Parse(resource.URI)
				if err != nil {
					slog.Error("failed to parse embedded resource URI", "uri", resource.URI, "error", err)
					panic(err)
				}
				if parsed.Path != "" {
					resourcePath = parsed.Path
				}
				mt := stringValue(resource.MimeType)
				if mt == "" {
					mt = mime.TypeByExtension(filepath.Ext(resourcePath))
				}
				if mt == "" {
					msg := fmt.Sprintf("could not detect MIME type for embedded resource URI %q", resource.URI)
					slog.Error(msg, "uri", resource.URI, "path", resourcePath)
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
					Content: gai.Str(resource.Blob),
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

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
