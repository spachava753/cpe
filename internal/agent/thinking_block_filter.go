package agent

import (
	"context"
	"slices"

	"github.com/spachava753/gai"
)

// ThinkingBlockFilter filters thinking blocks from the input dialog,
// keeping only those that originated from the specified generator types.
// This enables cross-model conversation resumption while preserving thinking
// blocks when switching back to earlier used models in the conversation.
//
// The filter uses gai.ThinkingExtraFieldGeneratorKey to identify which generator
// produced each thinking block. Thinking blocks without this key, or with a
// generator type not in the keepGeneratorTypes list, are filtered out.
type ThinkingBlockFilter struct {
	gai.GeneratorWrapper
	keepGeneratorTypes []string
}

// NewThinkingBlockFilter creates a new ThinkingBlockFilter that keeps thinking
// blocks from the specified generator types. All non-thinking blocks are preserved.
//
// Parameters:
//   - generator: The underlying generator to wrap
//   - keepGeneratorTypes: List of generator types to keep (e.g., gai.ThinkingGeneratorAnthropic).
//     If empty, all thinking blocks are filtered out.
func NewThinkingBlockFilter(generator gai.Generator, keepGeneratorTypes []string) *ThinkingBlockFilter {
	return &ThinkingBlockFilter{
		GeneratorWrapper:   gai.GeneratorWrapper{Inner: generator},
		keepGeneratorTypes: keepGeneratorTypes,
	}
}

// WithThinkingBlockFilter returns a WrapperFunc that filters thinking blocks,
// keeping only those from the specified generator types.
//
// Example usage:
//
//	wrapped := gai.Wrap(gen,
//	    WithThinkingBlockFilter(gai.ThinkingGeneratorAnthropic, gai.ThinkingGeneratorGemini),
//	)
func WithThinkingBlockFilter(keepGeneratorTypes ...string) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewThinkingBlockFilter(g, keepGeneratorTypes)
	}
}

// WithAnthropicThinkingFilter returns a WrapperFunc that keeps only Anthropic thinking blocks.
// This is a convenience wrapper for WithThinkingBlockFilter(gai.ThinkingGeneratorAnthropic).
func WithAnthropicThinkingFilter() gai.WrapperFunc {
	return WithThinkingBlockFilter(gai.ThinkingGeneratorAnthropic)
}

// WithGeminiThinkingFilter returns a WrapperFunc that keeps only Gemini thinking blocks.
// This is a convenience wrapper for WithThinkingBlockFilter(gai.ThinkingGeneratorGemini).
func WithGeminiThinkingFilter() gai.WrapperFunc {
	return WithThinkingBlockFilter(gai.ThinkingGeneratorGemini)
}

// WithOpenRouterThinkingFilter returns a WrapperFunc that keeps only OpenRouter thinking blocks.
// This is a convenience wrapper for WithThinkingBlockFilter(gai.ThinkingGeneratorOpenRouter).
func WithOpenRouterThinkingFilter() gai.WrapperFunc {
	return WithThinkingBlockFilter(gai.ThinkingGeneratorOpenRouter)
}

// WithResponsesThinkingFilter returns a WrapperFunc that keeps only OpenAI Responses thinking blocks.
// This is a convenience wrapper for WithThinkingBlockFilter(gai.ThinkingGeneratorResponses).
func WithResponsesThinkingFilter() gai.WrapperFunc {
	return WithThinkingBlockFilter(gai.ThinkingGeneratorResponses)
}

// WithCerebrasThinkingFilter returns a WrapperFunc that keeps only Cerebras thinking blocks.
// This is a convenience wrapper for WithThinkingBlockFilter(gai.ThinkingGeneratorCerebras).
func WithCerebrasThinkingFilter() gai.WrapperFunc {
	return WithThinkingBlockFilter(gai.ThinkingGeneratorCerebras)
}

// WithZaiThinkingFilter returns a WrapperFunc that keeps only Zai thinking blocks.
// This is a convenience wrapper for WithThinkingBlockFilter(gai.ThinkingGeneratorZai).
func WithZaiThinkingFilter() gai.WrapperFunc {
	return WithThinkingBlockFilter(gai.ThinkingGeneratorZai)
}

// Generate filters thinking blocks in the input dialog based on their generator type,
// then delegates to the inner generator.
func (f *ThinkingBlockFilter) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	filteredDialog := make(gai.Dialog, 0, len(dialog))
	for _, message := range dialog {
		filteredBlocks := make([]gai.Block, 0, len(message.Blocks))
		for _, block := range message.Blocks {
			if block.BlockType == gai.Thinking {
				// Check if this thinking block should be kept
				if f.shouldKeepThinkingBlock(block) {
					filteredBlocks = append(filteredBlocks, block)
				}
				// Otherwise filter it out
			} else {
				// Keep all other block types (Content, ToolCall, etc.)
				filteredBlocks = append(filteredBlocks, block)
			}
		}
		filteredMessage := gai.Message{
			Role:            message.Role,
			Blocks:          filteredBlocks,
			ToolResultError: message.ToolResultError,
			ExtraFields:     message.ExtraFields,
		}
		filteredDialog = append(filteredDialog, filteredMessage)
	}
	return f.GeneratorWrapper.Generate(ctx, filteredDialog, options)
}

// shouldKeepThinkingBlock checks if a thinking block should be kept based on
// the generator type in its ExtraFields.
func (f *ThinkingBlockFilter) shouldKeepThinkingBlock(block gai.Block) bool {
	if block.ExtraFields == nil {
		return false
	}

	generatorType, ok := block.ExtraFields[gai.ThinkingExtraFieldGeneratorKey].(string)
	if !ok {
		return false
	}

	return slices.Contains(f.keepGeneratorTypes, generatorType)
}
