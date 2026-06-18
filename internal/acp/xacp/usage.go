package xacp

import (
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/storage"
)

func PromptTurnUsage(dialog gai.Dialog) *acp.Usage {
	var usage acp.Usage
	var hasUsage bool
	var cacheReadTokens uint64
	var cacheWriteTokens uint64
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

func messageUsageInt(extra map[string]any, key string) (uint64, bool) {
	value, ok := extra[key]
	if !ok {
		return 0, false
	}
	intValue, ok := value.(int64)
	if !ok || intValue < 0 {
		return 0, false
	}
	return uint64(intValue), true
}
