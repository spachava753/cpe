package agent

import (
	"context"
	"strings"

	"github.com/spachava753/gai"
)

type blockKeepFunc func(gai.Block) bool

// BlockFilterWrapper filters input blocks before delegating generation to the
// wrapped generator.
type BlockFilterWrapper struct {
	gai.GeneratorWrapper
	keep blockKeepFunc
}

// NewBlockFilterWrapper returns a wrapper that keeps only blocks accepted by
// keep. When keep is nil, all blocks are preserved.
func NewBlockFilterWrapper(generator gai.Generator, keep blockKeepFunc) *BlockFilterWrapper {
	if keep == nil {
		keep = func(gai.Block) bool { return true }
	}
	return &BlockFilterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: generator},
		keep:             keep,
	}
}

func (f *BlockFilterWrapper) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	filteredDialog := make(gai.Dialog, 0, len(dialog))
	for _, message := range dialog {
		filteredBlocks := make([]gai.Block, 0, len(message.Blocks))
		for _, block := range message.Blocks {
			if f.keep(block) {
				filteredBlocks = append(filteredBlocks, block)
			}
		}
		if len(filteredBlocks) == 0 && message.Role != gai.ToolResult {
			continue
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

func whitelistBlockKeepFunc(allowedTypes []string) blockKeepFunc {
	allowed := make(map[string]struct{}, len(allowedTypes))
	for _, allowedType := range allowedTypes {
		allowed[allowedType] = struct{}{}
	}
	return func(block gai.Block) bool {
		_, ok := allowed[block.BlockType]
		return ok
	}
}

func thinkingBlockKeepFunc(keepGeneratorTypes []string) blockKeepFunc {
	allowedGenerators := make(map[string]struct{}, len(keepGeneratorTypes))
	for _, generatorType := range keepGeneratorTypes {
		allowedGenerators[generatorType] = struct{}{}
	}
	return func(block gai.Block) bool {
		if block.BlockType != gai.Thinking {
			return true
		}
		if block.ExtraFields == nil {
			return false
		}
		generatorType, ok := block.ExtraFields[gai.ThinkingExtraFieldGeneratorKey].(string)
		if !ok {
			return false
		}
		_, ok = allowedGenerators[generatorType]
		return ok
	}
}

// WithBlockFilter returns a WrapperFunc that applies the provider-specific
// input block filtering policy for the given model type.
func WithBlockFilter(modelType string) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return NewBlockFilterWrapper(g, providerBlockKeepFunc(modelType))
	}
}

func providerBlockKeepFunc(modelType string) blockKeepFunc {
	switch strings.ToLower(modelType) {
	case "anthropic":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorAnthropic})
	case "gemini":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorGemini})
	case "openrouter":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorOpenRouter})
	case "responses":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorResponses})
	case "cerebras":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorCerebras})
	case "zai":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorZai})
	case "openai", "groq":
		return whitelistBlockKeepFunc([]string{gai.Content, gai.ToolCall})
	default:
		return whitelistBlockKeepFunc([]string{gai.Content, gai.ToolCall})
	}
}
