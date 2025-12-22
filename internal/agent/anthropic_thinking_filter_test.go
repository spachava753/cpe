package agent

import (
	"context"
	"testing"

	"github.com/spachava753/gai"
)

func TestAnthropicThinkingBlockFilter(t *testing.T) {
	tests := []struct {
		name           string
		inputDialog    gai.Dialog
		expectedBlocks int // expected total blocks across all messages after filtering
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
			expectedBlocks: 2,
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
			expectedBlocks: 1,
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
			expectedBlocks: 1,
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
			expectedBlocks: 1,
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
			expectedBlocks: 2,
		},
		{
			name: "handles mixed conversation from multiple providers",
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						// Gemini thinking - should be filtered
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
						// Anthropic thinking - should be kept
						{
							BlockType:   gai.Thinking,
							Content:     gai.Str("anthropic thinking"),
							ExtraFields: map[string]interface{}{"anthropic_thinking_signature": "anth_sig"},
						},
						{BlockType: gai.Content, Content: gai.Str("anthropic response")},
					},
				},
			},
			expectedBlocks: 4, // gemini: 1 content, user: 1 content, anthropic: 2 blocks
		},
		{
			name:           "handles empty dialog",
			inputDialog:    gai.Dialog{},
			expectedBlocks: 0,
		},
		{
			name: "handles message with no blocks",
			inputDialog: gai.Dialog{
				{
					Role:   gai.Assistant,
					Blocks: []gai.Block{},
				},
			},
			expectedBlocks: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGenerator := &mockToolGenerator{}
			filter := NewAnthropicThinkingBlockFilter(mockGenerator)

			_, err := filter.Generate(context.Background(), tt.inputDialog, func(d gai.Dialog) *gai.GenOpts {
				return &gai.GenOpts{}
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Count total blocks across all messages
			totalBlocks := 0
			for _, msg := range mockGenerator.capturedDialog {
				totalBlocks += len(msg.Blocks)
			}

			if totalBlocks != tt.expectedBlocks {
				t.Errorf("expected %d blocks, got %d", tt.expectedBlocks, totalBlocks)
			}
		})
	}
}

func TestAnthropicThinkingBlockFilter_PreservesToolResultError(t *testing.T) {
	mockGenerator := &mockToolGenerator{}
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

	_, err := filter.Generate(context.Background(), inputDialog, func(d gai.Dialog) *gai.GenOpts {
		return &gai.GenOpts{}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockGenerator.capturedDialog) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mockGenerator.capturedDialog))
	}

	if !mockGenerator.capturedDialog[0].ToolResultError {
		t.Error("expected ToolResultError to be preserved as true")
	}
}

func TestAnthropicThinkingBlockFilter_Register(t *testing.T) {
	mockGenerator := &mockToolGenerator{}
	filter := NewAnthropicThinkingBlockFilter(mockGenerator)

	tool := gai.Tool{Name: "test_tool"}

	// mockToolGenerator doesn't implement ToolRegister, so this should return an error
	err := filter.Register(tool, nil)
	if err == nil {
		t.Error("expected error when underlying generator doesn't support tool registration")
	}
}

func TestAnthropicThinkingBlockFilter_FiltersOpenRouterThinking(t *testing.T) {
	mockGenerator := &mockToolGenerator{}
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

	_, err := filter.Generate(context.Background(), inputDialog, func(d gai.Dialog) *gai.GenOpts {
		return &gai.GenOpts{}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have 1 block (content), the OpenRouter thinking should be filtered
	totalBlocks := 0
	for _, msg := range mockGenerator.capturedDialog {
		totalBlocks += len(msg.Blocks)
	}

	if totalBlocks != 1 {
		t.Errorf("expected 1 block (OpenRouter thinking should be filtered), got %d", totalBlocks)
	}
}
