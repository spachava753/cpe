package xacp

import (
	"encoding/json"
	"fmt"
	"iter"
	"net/url"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
)

const pdfMIMEType = "application/pdf"

func MsgToSessionUpdate(msg gai.Message) iter.Seq[acp.SessionUpdate] {
	return func(yield func(acp.SessionUpdate) bool) {
		if msg.Role == gai.ToolResult {
			status := acp.ToolCallStatusCompleted
			if msg.ToolResultError {
				status = acp.ToolCallStatusFailed
			}
			type toolResultUpdate struct {
				update  acp.SessionUpdate
				content []acp.ToolCallContent
			}
			updates := make([]toolResultUpdate, 0, 1)
			byID := make(map[acp.ToolCallId]int)
			for _, b := range msg.Blocks {
				id := acp.ToolCallId(b.ID)
				idx, ok := byID[id]
				if !ok {
					idx = len(updates)
					byID[id] = idx
					update := acp.ToolCallUpdateSessionUpdate(id)
					update.Status = &status
					updates = append(updates, toolResultUpdate{update: update})
				}
				content := blockToContentBlock(b, b.Content.String())
				updates[idx].content = append(updates[idx].content, acp.ContentToolCallContent(content))
			}
			for _, result := range updates {
				result.update.Content = result.content
				if !yield(result.update) {
					return
				}
			}
			return
		}

		for _, b := range msg.Blocks {
			content := b.Content.String()
			acpBlock := blockToContentBlock(b, content)
			switch msg.Role {
			case gai.User:
				if !yield(acp.UserMessageChunkSessionUpdate(acpBlock)) {
					return
				}
			case gai.Assistant:
				switch b.BlockType {
				case gai.Thinking:
					if !yield(acp.AgentThoughtChunkSessionUpdate(acpBlock)) {
						return
					}
				case gai.ToolCall:
					var input gai.ToolCallInput
					if err := json.Unmarshal([]byte(content), &input); err != nil {
						panic(err)
					}
					// TODO: we should add support for tool kind
					// TODO: we should add support for file locations
					update := acp.ToolCallSessionUpdate(acp.ToolCallId(b.ID), input.Name)
					update.Status = new(acp.ToolCallStatusPending)
					update.RawInput = input.Parameters
					// Seed a text slot so clients can replace tool output in place.
					update.Content = []acp.ToolCallContent{
						acp.ContentToolCallContent(acp.TextContentBlock("")),
					}
					if !yield(update) {
						return
					}
				case gai.Content:
					if !yield(acp.AgentMessageChunkSessionUpdate(acpBlock)) {
						return
					}
				default:
					panic(fmt.Sprintf("unknown block type: %s", b.BlockType))
				}
			default:
				panic("unknown role")
			}
		}
	}
}

// BlocksToToolCallContent converts displayable GAI blocks into ACP tool-call content.
func BlocksToToolCallContent(blocks []gai.Block) []acp.ToolCallContent {
	content := make([]acp.ToolCallContent, len(blocks))
	for i, block := range blocks {
		acpBlock := blockToContentBlock(block, block.Content.String())
		content[i] = acp.ContentToolCallContent(acpBlock)
	}
	return content
}

func blockToContentBlock(b gai.Block, content string) acp.ContentBlock {
	switch b.ModalityType {
	case gai.Image:
		if b.MimeType == pdfMIMEType || b.MimeType == "application/x-pdf" {
			return blobResourceContentBlock(b, content, pdfMIMEType, "document.pdf")
		}
		return acp.ImageContentBlock(content, b.MimeType)
	case gai.Audio:
		return acp.AudioContentBlock(content, b.MimeType)
	case gai.Video:
		return blobResourceContentBlock(b, content, b.MimeType, "video")
	default:
		return acp.TextContentBlock(content)
	}
}

func blobResourceContentBlock(b gai.Block, content, mimeType, fallbackFilename string) acp.ContentBlock {
	filename, _ := b.ExtraFields[gai.BlockFieldFilenameKey].(string)
	if filename == "" {
		filename = fallbackFilename
	}
	resource := acp.BlobResourceContentsEmbeddedResourceResource(
		content,
		"artifact:///"+url.PathEscape(filename),
	)
	resource.MimeType = new(mimeType)
	return acp.ResourceContentBlock(resource)
}
