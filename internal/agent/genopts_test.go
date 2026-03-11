package agent

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/storage"
)

func TestBuildGenOptsForDialog_ResponsesAddsPromptCacheKeyWithoutMutatingBase(t *testing.T) {
	base := &gai.GenOpts{ThinkingBudget: "medium"}
	dialog := gai.Dialog{{
		Role:   gai.User,
		Blocks: []gai.Block{gai.TextBlock("hello")},
	}}

	opts := BuildGenOptsForDialog(config.Model{Type: ModelTypeResponses, ID: "gpt-5"}, base, dialog)
	if opts == nil {
		t.Fatal("BuildGenOptsForDialog returned nil")
	}
	if opts == base {
		t.Fatal("BuildGenOptsForDialog reused the base options pointer")
	}
	if base.ExtraArgs != nil {
		t.Fatalf("BuildGenOptsForDialog mutated base ExtraArgs: %#v", base.ExtraArgs)
	}

	cacheKey, ok := opts.ExtraArgs[gai.ResponsesPromptCacheKeyParam].(string)
	if !ok || cacheKey == "" {
		t.Fatalf("missing prompt cache key: %#v", opts.ExtraArgs)
	}
	if got := opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]; got != responses.ReasoningSummaryDetailed {
		t.Fatalf("unexpected reasoning summary detail: %#v", got)
	}
}

func TestBuildGenOptsForDialog_PreservesExplicitPromptCacheKey(t *testing.T) {
	base := &gai.GenOpts{
		ExtraArgs: map[string]any{
			gai.ResponsesPromptCacheKeyParam: "manual-key",
		},
	}
	dialog := gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}

	opts := BuildGenOptsForDialog(config.Model{Type: ModelTypeResponses, ID: "gpt-5"}, base, dialog)
	if got := opts.ExtraArgs[gai.ResponsesPromptCacheKeyParam]; got != "manual-key" {
		t.Fatalf("unexpected prompt cache key: %#v", got)
	}
}

func TestResponsesPromptCacheKey_IgnoresStorageMetadataAndThinking(t *testing.T) {
	callA := mustPromptCacheToolCallBlock(t, "call-1", "search", map[string]any{"query": "prompt caching"})
	callB := mustPromptCacheToolCallBlock(t, "call-1", "search", map[string]any{"query": "prompt caching"})

	dialogA := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				gai.TextBlock("find recent prompt caching docs"),
			},
			ExtraFields: map[string]any{storage.MessageIDKey: "user-a"},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:    gai.Thinking,
					ModalityType: gai.Text,
					MimeType:     "text/plain",
					Content:      gai.Str("hidden reasoning"),
					ExtraFields: map[string]any{
						gai.ThinkingExtraFieldGeneratorKey: gai.ThinkingGeneratorResponses,
					},
				},
				callA,
			},
			ExtraFields: map[string]any{storage.MessageIDKey: "assistant-a"},
		},
		{
			Role: gai.ToolResult,
			Blocks: []gai.Block{{
				ID:           "call-1",
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("found docs"),
			}},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				gai.TextBlock("Here are the docs."),
			},
		},
		{
			Role: gai.User,
			Blocks: []gai.Block{
				gai.TextBlock("summarize them"),
			},
			ExtraFields: map[string]any{storage.MessageParentIDKey: "assistant-a"},
		},
	}

	dialogB := gai.Dialog{
		{
			Role:        gai.User,
			Blocks:      []gai.Block{gai.TextBlock("find recent prompt caching docs")},
			ExtraFields: map[string]any{storage.MessageIDKey: "user-b"},
		},
		{
			Role:        gai.Assistant,
			Blocks:      []gai.Block{callB},
			ExtraFields: map[string]any{storage.MessageIDKey: "assistant-b"},
		},
		{
			Role: gai.ToolResult,
			Blocks: []gai.Block{{
				ID:           "call-1",
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("found docs"),
			}},
		},
		{
			Role:   gai.Assistant,
			Blocks: []gai.Block{gai.TextBlock("Here are the docs.")},
		},
		{
			Role:        gai.User,
			Blocks:      []gai.Block{gai.TextBlock("summarize them")},
			ExtraFields: map[string]any{storage.MessageParentIDKey: "assistant-b"},
		},
	}

	keyA := responsesPromptCacheKey("gpt-5", dialogA)
	keyB := responsesPromptCacheKey("gpt-5", dialogB)
	if keyA != keyB {
		t.Fatalf("expected identical cache keys, got %q != %q", keyA, keyB)
	}
}

func TestResponsesPromptCacheKey_UsesPrefixThroughLatestUser(t *testing.T) {
	prefix := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("first")}},
		{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("reply")}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}
	withInFlightAssistant := append(append(gai.Dialog{}, prefix...), gai.Message{
		Role: gai.Assistant,
		Blocks: []gai.Block{{
			BlockType:    gai.Thinking,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("temporary reasoning"),
		}},
	})

	keyPrefix := responsesPromptCacheKey("gpt-5", prefix)
	keyInFlight := responsesPromptCacheKey("gpt-5", withInFlightAssistant)
	if keyPrefix != keyInFlight {
		t.Fatalf("expected latest-user prefix cache key to ignore in-flight assistant data, got %q != %q", keyPrefix, keyInFlight)
	}

	if otherModel := responsesPromptCacheKey("gpt-5-mini", prefix); otherModel == keyPrefix {
		t.Fatal("expected model ID to affect the prompt cache key")
	}
}

func TestResponsesPromptCacheKey_ChangesWhenToolIDsChange(t *testing.T) {
	dialogA := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustPromptCacheToolCallBlock(t, "call-1", "lookup", map[string]any{"query": "docs"})}},
		{Role: gai.ToolResult, Blocks: []gai.Block{{
			ID:           "call-1",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("result"),
		}}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}
	dialogB := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustPromptCacheToolCallBlock(t, "call-2", "lookup", map[string]any{"query": "docs"})}},
		{Role: gai.ToolResult, Blocks: []gai.Block{{
			ID:           "call-2",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("result"),
		}}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}

	keyA := responsesPromptCacheKey("gpt-5", dialogA)
	keyB := responsesPromptCacheKey("gpt-5", dialogB)
	if keyA == keyB {
		t.Fatalf("expected tool IDs to affect the cache key, got %q", keyA)
	}
}

func TestResponsesPromptCacheKey_IgnoresToolResultErrorFlag(t *testing.T) {
	dialogA := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.ToolResult, ToolResultError: false, Blocks: []gai.Block{{
			ID:           "call-1",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("same content"),
		}}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}
	dialogB := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.ToolResult, ToolResultError: true, Blocks: []gai.Block{{
			ID:           "call-1",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("same content"),
		}}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}

	keyA := responsesPromptCacheKey("gpt-5", dialogA)
	keyB := responsesPromptCacheKey("gpt-5", dialogB)
	if keyA != keyB {
		t.Fatalf("expected tool result error flag to be ignored, got %q != %q", keyA, keyB)
	}
}

func TestResponsesPromptCacheKey_PreservesLargeIntegerToolParameters(t *testing.T) {
	const largeID int64 = 9007199254740993

	dialogA := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustPromptCacheToolCallBlock(t, "call-1", "lookup", map[string]any{"id": largeID})}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}
	dialogB := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.Assistant, Blocks: []gai.Block{mustPromptCacheToolCallBlock(t, "call-1", "lookup", map[string]any{"id": largeID + 1})}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}

	keyA := responsesPromptCacheKey("gpt-5", dialogA)
	keyB := responsesPromptCacheKey("gpt-5", dialogB)
	if keyA == keyB {
		t.Fatalf("expected distinct large integer tool parameters to produce different cache keys, got %q", keyA)
	}
}

func TestResponsesPromptCacheKey_IgnoresTextMimeDifferences(t *testing.T) {
	dialogA := gai.Dialog{{
		Role: gai.User,
		Blocks: []gai.Block{{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("hello"),
		}},
	}}
	dialogB := gai.Dialog{{
		Role: gai.User,
		Blocks: []gai.Block{{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/markdown",
			Content:      gai.Str("hello"),
		}},
	}}

	keyA := responsesPromptCacheKey("gpt-5", dialogA)
	keyB := responsesPromptCacheKey("gpt-5", dialogB)
	if keyA != keyB {
		t.Fatalf("expected text mime differences to be ignored, got %q != %q", keyA, keyB)
	}
}

func TestResponsesPromptCacheKey_CanonicalizesAllTextToolResultsLikeResponsesInput(t *testing.T) {
	dialogA := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.ToolResult, Blocks: []gai.Block{{
			ID:           "call-1",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("a"),
		}, {
			ID:           "call-1",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("b"),
		}}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}
	dialogB := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("call tool")}},
		{Role: gai.ToolResult, Blocks: []gai.Block{{
			ID:           "call-1",
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			MimeType:     "text/plain",
			Content:      gai.Str("ab"),
		}}},
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("follow up")}},
	}

	keyA := responsesPromptCacheKey("gpt-5", dialogA)
	keyB := responsesPromptCacheKey("gpt-5", dialogB)
	if keyA != keyB {
		t.Fatalf("expected split and merged all-text tool results to hash the same, got %q != %q", keyA, keyB)
	}
}

func mustPromptCacheToolCallBlock(t *testing.T, id, name string, params map[string]any) gai.Block {
	t.Helper()
	content, err := json.Marshal(gai.ToolCallInput{Name: name, Parameters: params})
	if err != nil {
		t.Fatalf("marshal tool call: %v", err)
	}
	return gai.Block{
		ID:           id,
		BlockType:    gai.ToolCall,
		ModalityType: gai.Text,
		MimeType:     "application/json",
		Content:      gai.Str(content),
	}
}
