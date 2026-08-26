package agent

import (
	"context"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
)

const (
	blockModelRefKey    = "cpe_block_model_ref"
	blockProviderURLKey = "cpe_block_provider_url"
)

type blockProvenance struct {
	modelRef    string
	providerURL string
}

type blockKeepFunc func(gai.Block) bool

// blockFilterWrapper filters input blocks before delegating generation to the
// wrapped generator.
type blockFilterWrapper struct {
	gai.GeneratorWrapper
	keep blockKeepFunc
}

// newBlockFilterWrapper returns a wrapper that keeps only blocks accepted by
// keep. When keep is nil, all blocks are preserved.
func newBlockFilterWrapper(generator gai.Generator, keep blockKeepFunc) *blockFilterWrapper {
	if keep == nil {
		keep = func(gai.Block) bool { return true }
	}
	return &blockFilterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: generator},
		keep:             keep,
	}
}

func (f *blockFilterWrapper) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
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

// ApplyBlockProvenance records the model profile and configured provider URL
// that produced every block in an assistant message.
func ApplyBlockProvenance(message *gai.Message, model config.Model) {
	for i := range message.Blocks {
		if message.Blocks[i].ExtraFields == nil {
			message.Blocks[i].ExtraFields = make(map[string]any)
		}
		message.Blocks[i].ExtraFields[blockModelRefKey] = model.Ref
		message.Blocks[i].ExtraFields[blockProviderURLKey] = model.BaseUrl
	}
}

func provenanceForBlock(block gai.Block) (blockProvenance, bool) {
	if block.ExtraFields == nil {
		return blockProvenance{}, false
	}
	modelRef, hasModelRef := block.ExtraFields[blockModelRefKey].(string)
	providerURL, hasProviderURL := block.ExtraFields[blockProviderURLKey].(string)
	if !hasModelRef || !hasProviderURL {
		return blockProvenance{}, false
	}
	return blockProvenance{modelRef: modelRef, providerURL: providerURL}, true
}

func provenanceForModel(model config.Model) blockProvenance {
	return blockProvenance{modelRef: model.Ref, providerURL: model.BaseUrl}
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

func thinkingBlockKeepFunc(keepGeneratorTypes []string, model config.Model) blockKeepFunc {
	allowedGenerators := make(map[string]struct{}, len(keepGeneratorTypes))
	for _, generatorType := range keepGeneratorTypes {
		allowedGenerators[generatorType] = struct{}{}
	}
	target := provenanceForModel(model)
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
		if _, ok = allowedGenerators[generatorType]; !ok {
			return false
		}
		provenance, ok := provenanceForBlock(block)
		return ok && provenance.modelRef == target.modelRef && provenance.providerURL == target.providerURL
	}
}

// WithBlockFilter returns a WrapperFunc that applies the provider-specific
// input block filtering policy for the selected model profile.
func WithBlockFilter(model config.Model) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return newBlockFilterWrapper(g, providerBlockKeepFunc(model))
	}
}

func providerBlockKeepFunc(model config.Model) blockKeepFunc {
	switch strings.ToLower(model.Type) {
	case "anthropic", "anthropic_vertex":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorAnthropic}, model)
	case "gemini":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorGemini}, model)
	case "openrouter":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorOpenRouter}, model)
	case "responses":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorResponses}, model)
	case "cerebras":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorCerebras}, model)
	case "zai":
		return thinkingBlockKeepFunc([]string{gai.ThinkingGeneratorZai}, model)
	case "openai", "groq":
		return whitelistBlockKeepFunc([]string{gai.Content, gai.ToolCall})
	default:
		return whitelistBlockKeepFunc([]string{gai.Content, gai.ToolCall})
	}
}
