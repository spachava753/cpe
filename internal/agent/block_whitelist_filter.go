package agent

import (
	"context"
	"fmt"

	"github.com/spachava753/gai"
)

// ToolRegister interface for registering tools
type ToolRegister interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

// BlockWhitelistFilter wraps a AgentGenerator and filters blocks based on a whitelist of allowed block types.
// Only blocks whose BlockType is in the AllowedTypes slice will be kept.
type BlockWhitelistFilter struct {
	generator    AgentGenerator
	allowedTypes []string
}

// NewBlockWhitelistFilter creates a new BlockWhitelistFilter with the specified allowed block types.
// If allowedTypes is empty, all blocks are filtered out (whitelist behavior).
func NewBlockWhitelistFilter(generator AgentGenerator, allowedTypes []string) *BlockWhitelistFilter {
	return &BlockWhitelistFilter{
		generator:    generator,
		allowedTypes: allowedTypes,
	}
}

// Generate wraps the AgentGenerator.Generate method and filters blocks based on the whitelist
func (f *BlockWhitelistFilter) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	// Filter blocks in each message based on whitelist
	filteredDialog := make(gai.Dialog, 0, len(dialog))
	for _, message := range dialog {
		filteredBlocks := make([]gai.Block, 0)
		for _, block := range message.Blocks {
			if f.isAllowed(block.BlockType) {
				filteredBlocks = append(filteredBlocks, block)
			}
		}

		// Create a new message with filtered blocks
		filteredMessage := gai.Message{
			Role:            message.Role,
			Blocks:          filteredBlocks,
			ToolResultError: message.ToolResultError,
		}
		filteredDialog = append(filteredDialog, filteredMessage)
	}

	// Call the wrapped generator
	return f.generator.Generate(ctx, filteredDialog, optsGen)
}

// Register delegates to the underlying generator if it supports tool registration
func (f *BlockWhitelistFilter) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if toolRegister, ok := f.generator.(ToolRegister); ok {
		return toolRegister.Register(tool, callback)
	}
	return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
}

// isAllowed checks if a block type is in the whitelist
func (f *BlockWhitelistFilter) isAllowed(blockType string) bool {
	for _, allowed := range f.allowedTypes {
		if allowed == blockType {
			return true
		}
	}
	return false
}
