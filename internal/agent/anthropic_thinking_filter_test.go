package agent

import (
	"context"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

func TestAnthropicThinkingBlockFilter(t *testing.T) {
	tests := []struct {
		name        string
		inputDialog gai.Dialog
	}{
		{
			name: "keeps Anthropic thinking blocks",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("thinking..."),
							ExtraFields: map[string]interface{}{"anthropic_thinking_signature": "sig123"},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name: "filters out Gemini thinking blocks",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("thinking..."),
							ExtraFields: map[string]interface{}{"gemini_thought_signature": "sig456"},
						},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name: "filters out thinking blocks without any signature",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Thinking, Content: gai.Str("thinking...")},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name: "filters out thinking blocks with nil ExtraFields",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Thinking, Content: gai.Str("thinking..."), ExtraFields: nil},
						{BlockType: gai.Content, Content: gai.Str("response")},
					},
				},
			},
		},
		{
			name: "preserves Content and ToolCall blocks unchanged",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Content, Content: gai.Str("text")},
						{BlockType: gai.ToolCall, Content: gai.Str("tool"), ID: "call_123"},
					},
				},
			},
		},
		{
			name: "handles mixed conversation from multiple providers",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("gemini thinking"),
							ExtraFields: map[string]interface{}{"gemini_thought_signature": "gem_sig"},
						},
						{BlockType: gai.Content, Content: gai.Str("gemini response")},
					},
				},
				{
					Role: gai.User,
					Blocks: []gai.Block{
						{BlockType: gai.Content, Content: gai.Str("user message")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("anthropic thinking"),
							ExtraFields: map[string]interface{}{"anthropic_thinking_signature": "anth_sig"},
						},
						{BlockType: gai.Content, Content: gai.Str("anthropic response")},
					},
				},
			},
		},
		{
			name:        "handles empty dialog",
			inputDialog: gai.Dialog{},
		},
		{
			name: "handles message with no blocks",
			inputDialog: gai.Dialog{
				{
					Role:   gai.Assistant,
					Blocks: []gai.Block{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGenerator := &mockGaiGenerator{}
			filter := NewAnthropicThinkingBlockFilter(mockGenerator)

			_, err := filter.Generate(context.Background(), tt.inputDialog, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
		})
	}
}

func TestAnthropicThinkingBlockFilter_PreservesToolResultError(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewAnthropicThinkingBlockFilter(mockGenerator)

	inputDialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, Content: gai.Str("error message")},
			},
			ToolResultError: true,
		},
	}

	_, err := filter.Generate(context.Background(), inputDialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
}

func TestAnthropicThinkingBlockFilter_Register(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewAnthropicThinkingBlockFilter(mockGenerator)

	tool := gai.Tool{Name: "test_tool"}

	// mockGaiGenerator doesn't implement ToolRegister, so this should return an error
	err := filter.Register(tool)
	if err == nil {
		t.Error("expected error when underlying generator doesn't support tool registration")
	}
}

func TestAnthropicThinkingBlockFilter_FiltersOpenRouterThinking(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewAnthropicThinkingBlockFilter(mockGenerator)

	inputDialog := gai.Dialog{
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					BlockType:   gai.Thinking,
					Content:     gai.Str("openrouter reasoning..."),
					ExtraFields: map[string]interface{}{"reasoning_type": "deepseek"},
				},
				{BlockType: gai.Content, Content: gai.Str("response")},
			},
		},
	}

	_, err := filter.Generate(context.Background(), inputDialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
}
