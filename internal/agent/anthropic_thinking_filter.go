package agent

import (
	"context"

	"github.com/spachava753/gai"
)

const anthropicThinkingSignatureKey = "anthropic_thinking_signature"

// AnthropicThinkingBlockFilter filters thinking blocks from the input dialog,
// keeping only those that originated from Anthropic (identified by the
// anthropic_thinking_signature key). This ensures cross-provider compatibility
// when resuming conversations.
type AnthropicThinkingBlockFilter struct {
	gai.GeneratorWrapper
}

func NewAnthropicThinkingBlockFilter(generator gai.Generator) *AnthropicThinkingBlockFilter {
	return &AnthropicThinkingBlockFilter{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: generator},
	}
}

// WithAnthropicThinkingFilter returns a WrapperFunc for use with gai.Wrap
func WithAnthropicThinkingFilter() gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewAnthropicThinkingBlockFilter(g)
	}
}

func (f *AnthropicThinkingBlockFilter) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
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
	return f.GeneratorWrapper.Generate(ctx, filteredDialog, options)
}
