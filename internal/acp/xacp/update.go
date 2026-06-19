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
