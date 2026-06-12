package xacp

import (
	"reflect"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/gai"
)

func TestPromptTurnUsage(t *testing.T) {
	usage := PromptTurnUsage(gai.Dialog{
		{
			Role: gai.Assistant,
			ExtraFields: map[string]any{
				storage.AgentMetadataInputTokensKey:      int64(80),
				storage.AgentMetadataOutputTokensKey:     int64(10),
				storage.AgentMetadataCacheReadTokensKey:  int64(30),
				storage.AgentMetadataCacheWriteTokensKey: int64(4),
			},
		},
		{
			Role: gai.Assistant,
			ExtraFields: map[string]any{
				storage.AgentMetadataInputTokensKey:     int64(5),
				storage.AgentMetadataOutputTokensKey:    int64(2),
				storage.AgentMetadataCacheReadTokensKey: int64(1),
			},
		},
	})

	cachedReadTokens := 31
	cachedWriteTokens := 4
	want := &acpsdk.Usage{
		TotalTokens:       97,
		InputTokens:       85,
		OutputTokens:      12,
		CachedReadTokens:  &cachedReadTokens,
		CachedWriteTokens: &cachedWriteTokens,
	}
	if !reflect.DeepEqual(usage, want) {
		t.Fatalf("promptTurnUsage() = %#v, want %#v", usage, want)
	}
	if got := PromptTurnUsage(gai.Dialog{{Role: gai.Assistant, ExtraFields: map[string]any{}}}); got != nil {
		t.Fatalf("promptTurnUsage() without metadata = %#v, want nil", got)
	}
}
