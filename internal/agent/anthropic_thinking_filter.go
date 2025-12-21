package agent

import (
	"context"
	"fmt"

	"github.com/spachava753/gai"
)

const anthropicThinkingSignatureKey = "anthropic_thinking_signature"

// AnthropicThinkingBlockFilter filters thinking blocks, keeping only those
// that originated from Anthropic (identified by the anthropic_thinking_signature key).
// This ensures cross-provider compatibility when resuming conversations.
type AnthropicThinkingBlockFilter struct {
	generator Iface
}

func NewAnthropicThinkingBlockFilter(generator Iface) *AnthropicThinkingBlockFilter {
	return &AnthropicThinkingBlockFilter{generator: generator}
}

func (f *AnthropicThinkingBlockFilter) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	filteredDialog := make(gai.Dialog, 0, len(dialog))
	for _, message := range dialog {
		filteredBlocks := make([]gai.Block, 0, len(message.Blocks))
		for _, block := range message.Blocks {
			switch block.BlockType {
			case gai.Thinking:
				// Only keep thinking blocks that have Anthropic signature
				if block.ExtraFields != nil {
					if _, hasAnthropicSig := block.ExtraFields[anthropicThinkingSignatureKey]; hasAnthropicSig {
						filteredBlocks = append(filteredBlocks, block)
					}
				}
				// Otherwise filter it out (it's from another provider or has no signature)
			default:
				// Keep all other block types (Content, ToolCall, etc.)
				filteredBlocks = append(filteredBlocks, block)
			}
		}
		filteredMessage := gai.Message{
			Role:            message.Role,
			Blocks:          filteredBlocks,
			ToolResultError: message.ToolResultError,
		}
		filteredDialog = append(filteredDialog, filteredMessage)
	}
	return f.generator.Generate(ctx, filteredDialog, optsGen)
}

// Register delegates to the underlying generator if it supports tool registration
func (f *AnthropicThinkingBlockFilter) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if toolRegister, ok := f.generator.(ToolRegister); ok {
		return toolRegister.Register(tool, callback)
	}
	return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
}
