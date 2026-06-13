package xacp

import (
	"github.com/coder/acp-go-sdk"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func PromptTurnUsage(dialog gai.Dialog) *acp.Usage {
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
	intValue, ok := value.(int64)
	if !ok {
		return 0, false
	}
	return int(intValue), true
}
