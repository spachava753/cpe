package xacp

import (
	"encoding/json"
	"fmt"
	"iter"

	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
)

func MsgToSessionUpdate(msg gai.Message) iter.Seq[acp.SessionUpdate] {
	return func(yield func(acp.SessionUpdate) bool) {
		for _, b := range msg.Blocks {
			content := b.Content.String()
			acpBlock := blockToContentBlock(b, content)
			switch msg.Role {
			case gai.User:
				if isEmptyTextContentBlock(acpBlock) {
					// ACP requires the `text` field on text content blocks, but
					// acp.ContentBlock serializes Text with `omitempty`, so an
					// empty text block would marshal as `{"type":"text"}` and be
					// rejected by clients with -32602 "missing field `text`".
					continue
				}
				if !yield(acp.UserMessageChunkSessionUpdate(acpBlock)) {
					return
				}
			case gai.Assistant:
				switch b.BlockType {
				case gai.Thinking:
					if isEmptyTextContentBlock(acpBlock) {
						continue
					}
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
					if !yield(update) {
						return
					}
				case gai.Content:
					if isEmptyTextContentBlock(acpBlock) {
						continue
					}
					if !yield(acp.AgentMessageChunkSessionUpdate(acpBlock)) {
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
				content := []acp.ToolCallContent{acp.ContentToolCallContent(acpBlock)}
				update := acp.ToolCallUpdateSessionUpdate(acp.ToolCallId(b.ID))
				update.Status = &status
				update.Content = content
				if !yield(update) {
					return
				}
			default:
				panic("unknown role")
			}
		}
	}
}

// isEmptyTextContentBlock reports whether b is a text content block whose text
// payload is empty. Such blocks must not be emitted as session updates because
// the ACP schema requires the `text` field, but acp.ContentBlock marshals it
// with `omitempty`, producing an invalid `{"type":"text"}` payload.
func isEmptyTextContentBlock(b acp.ContentBlock) bool {
	return b.Type == acp.ContentBlockTypeText && b.Text == ""
}

func blockToContentBlock(b gai.Block, content string) acp.ContentBlock {
	switch b.ModalityType {
	case gai.Image:
		return acp.ImageContentBlock(content, b.MimeType)
	case gai.Audio:
		return acp.AudioContentBlock(content, b.MimeType)
	default:
		return acp.TextContentBlock(content)
	}
}
