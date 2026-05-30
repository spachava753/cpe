package acp

import (
	"encoding/json"
	"fmt"
	"iter"

	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/cpe/internal/storage"
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
			block = gai.ImageBlock([]byte(contentBlock.Image.Data), contentBlock.Image.MimeType)
		case contentBlock.Audio != nil:
			block = gai.AudioBlock([]byte(contentBlock.Audio.Data), contentBlock.Audio.MimeType)
		case contentBlock.ResourceLink != nil: // TODO: support resource links better
			block = gai.TextBlock(fmt.Sprintf("Resource %s: %s", contentBlock.ResourceLink.Name, contentBlock.ResourceLink.Uri))
		case contentBlock.Resource != nil: // TODO: support embedded resources better
			resource := contentBlock.Resource.Resource
			if resource.TextResourceContents != nil {
				block = gai.TextBlock(resource.TextResourceContents.Text)
			}
			if resource.BlobResourceContents != nil {
				block = gai.TextBlock(fmt.Sprintf("Resource %s: %s", resource.BlobResourceContents.Uri, resource.BlobResourceContents.Blob))
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
					// TODO: we should add support for diff content blocks based on calls to text_edit tool
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
	return int(value.(int64)), true
}
