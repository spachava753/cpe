package agent

import (
	"context"
	"slices"

	"github.com/spachava753/gai"
)

// BlockWhitelistFilter wraps a generator and filters blocks from the input dialog
// based on a whitelist of allowed block types.
type BlockWhitelistFilter struct {
	gai.GeneratorWrapper
	allowedTypes []string
}

// NewBlockWhitelistFilter creates a new BlockWhitelistFilter with the specified allowed block types.
func NewBlockWhitelistFilter(generator gai.Generator, allowedTypes []string) *BlockWhitelistFilter {
	return &BlockWhitelistFilter{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: generator},
		allowedTypes:     allowedTypes,
	}
}

// WithBlockWhitelist returns a WrapperFunc for use with gai.Wrap
func WithBlockWhitelist(allowedTypes []string) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewBlockWhitelistFilter(g, allowedTypes)
	}
}

// Generate filters blocks in the input dialog based on the whitelist, then delegates to the inner generator
func (f *BlockWhitelistFilter) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	filteredDialog := make(gai.Dialog, 0, len(dialog))
	for _, message := range dialog {
		filteredBlocks := make([]gai.Block, 0)
		for _, block := range message.Blocks {
			if slices.Contains(f.allowedTypes, block.BlockType) {
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
