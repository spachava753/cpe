package acp

import (
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
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

func msgToSessionUpdate(msg gai.Message) iter.Seq[acp.SessionUpdate] {
	return func(yield func(acp.SessionUpdate) bool) {
		for _, b := range msg.Blocks {
			content := b.Content.String()
			var acpBlock acp.ContentBlock
			switch b.ModalityType {
			case gai.Image:
				acpBlock = acp.ImageBlock(content, b.MimeType)
			case gai.Audio:
				acpBlock = acp.AudioBlock(content, b.MimeType)
			default:
				acpBlock = acp.TextBlock(content)
			}
			switch msg.Role {
			case gai.User:
				if !yield(acp.UpdateUserMessage(acpBlock)) {
					return
				}
			case gai.Assistant:
				switch b.BlockType {
				case gai.Thinking:
					if !yield(acp.UpdateAgentThought(acpBlock)) {
						return
					}
				case gai.ToolCall:
					var input gai.ToolCallInput
					if err := json.Unmarshal([]byte(content), &input); err != nil {
						panic(err)
					}
					// TODO: we should add support for tool kind
					// TODO: we should add support for file locations
					if !yield(acp.StartToolCall(
						acp.ToolCallId(b.ID),
						input.Name,
						acp.WithStartStatus(acp.ToolCallStatusPending),
						acp.WithStartRawInput(input.Parameters),
					)) {
						return
					}
				case gai.Content:
					if !yield(acp.UpdateAgentMessage(acpBlock)) {
						return
					}
				default:
					panic(fmt.Sprintf("unknown block type: %s", b.BlockType))
				}
			case gai.ToolResult:
				status := acp.ToolCallStatusCompleted
				if msg.ToolResultError {
					status = acp.ToolCallStatusFailed
				}
				if !yield(acp.UpdateToolCall(
					acp.ToolCallId(b.ID),
					acp.WithUpdateStatus(status),
					acp.WithUpdateContent([]acp.ToolCallContent{
						acp.ToolContent(acpBlock),
					}),
				)) {
					return
				}
			default:
				panic("unknown role")
			}
		}
	}
}

func promptTurnDialog(dialog gai.Dialog, inputLen int) gai.Dialog {
	// Compaction can replace the input history with a shorter rebased dialog.
	// In that case, the returned dialog is the only safe turn boundary we have.
	if inputLen < 0 || inputLen > len(dialog) {
		return dialog
	}
	return dialog[inputLen:]
}

func promptTurnUsage(dialog gai.Dialog) *acp.Usage {
	var usage acp.Usage
	var hasUsage bool
	var cacheReadTokens int
	var cacheWriteTokens int
	var hasCacheReadTokens bool
	var hasCacheWriteTokens bool

	for _, msg := range dialog {
		if msg.Role != gai.Assistant {
			continue
		}
		if value, ok := messageUsageInt(msg.ExtraFields, storage.AgentMetadataInputTokensKey); ok {
			usage.InputTokens += value
			hasUsage = true
		}
		if value, ok := messageUsageInt(msg.ExtraFields, storage.AgentMetadataOutputTokensKey); ok {
			usage.OutputTokens += value
			hasUsage = true
		}
		if value, ok := messageUsageInt(msg.ExtraFields, storage.AgentMetadataCacheReadTokensKey); ok {
			cacheReadTokens += value
			hasUsage = true
			hasCacheReadTokens = true
		}
		if value, ok := messageUsageInt(msg.ExtraFields, storage.AgentMetadataCacheWriteTokensKey); ok {
			cacheWriteTokens += value
			hasUsage = true
			hasCacheWriteTokens = true
		}
	}

	if !hasUsage {
		return nil
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	if hasCacheReadTokens {
		usage.CachedReadTokens = &cacheReadTokens
	}
	if hasCacheWriteTokens {
		usage.CachedWriteTokens = &cacheWriteTokens
	}
	return &usage
}

func messageUsageInt(extra map[string]any, key string) (int, bool) {
	value, ok := extra[key]
	if !ok {
		return 0, false
	}
	intValue, ok := value.(int64)
	if !ok {
		return 0, false
	}
	return int(intValue), true
}
