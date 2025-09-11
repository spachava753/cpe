package agent

import (
	"context"
	"testing"

	"github.com/spachava753/gai"
)

func TestBlockWhitelistFilter(t *testing.T) {
	tests := []struct {
		name         string
		allowedTypes []string
		messages     []gai.Message
		expected     int // expected number of blocks in first message
	}{
		{
			name:         "filters out thinking blocks when not in whitelist",
			allowedTypes: []string{"content"},
			messages: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: "content", Content: gai.Str("Hello")},
						{BlockType: "thinking", Content: gai.Str("Let me think about this")},
						{BlockType: "content", Content: gai.Str("World")},
					},
				},
			},
			expected: 2, // Only content blocks should remain
		},
		{
			name:         "keeps only whitelisted blocks",
			allowedTypes: []string{"content"},
			messages: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: "content", Content: gai.Str("Hello")},
						{BlockType: "tool", Content: gai.Str("tool call")},
						{BlockType: "content", Content: gai.Str("World")},
					},
				},
			},
			expected: 2, // Only content blocks should remain
		},
		{
			name:         "filters all blocks when whitelist is empty",
			allowedTypes: []string{},
			messages: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: "content", Content: gai.Str("Hello")},
						{BlockType: "thinking", Content: gai.Str("Let me think about this")},
						{BlockType: "tool", Content: gai.Str("tool call")},
					},
				},
			},
			expected: 0, // No blocks should remain when whitelist is empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock generator that returns our test messages
			mockGenerator := &mockToolGenerator{
				response: tt.messages,
			}

			// Create the filter
			filter := NewBlockWhitelistFilter(mockGenerator, tt.allowedTypes)

			// Generate
			result, err := filter.Generate(context.Background(), gai.Dialog{}, func(d gai.Dialog) *gai.GenOpts {
				return &gai.GenOpts{}
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify the result
			if len(result) == 0 {
				t.Fatalf("expected at least one message, got 0")
			}

			if len(result[0].Blocks) != tt.expected {
				t.Errorf("expected %d blocks, got %d", tt.expected, len(result[0].Blocks))
			}
		})
	}
}

// mockToolGenerator is a mock implementation of ToolGenerator for testing
type mockToolGenerator struct {
	response gai.Dialog
}

func (m *mockToolGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	return m.response, nil
}