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
		inputDialog  gai.Dialog
		expected     int // expected number of blocks in first message after filtering
	}{
		{
			name:         "filters out thinking blocks when not in whitelist",
			allowedTypes: []string{"content"},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: "content", Content: gai.Str("Hello")},
						{BlockType: "thinking", Content: gai.Str("Let me think about this")},
						{BlockType: "content", Content: gai.Str("World")},
					},
				},
			},
			expected: 2, // Only content blocks should remain after filtering
		},
		{
			name:         "keeps only whitelisted blocks",
			allowedTypes: []string{"content"},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: "content", Content: gai.Str("Hello")},
						{BlockType: "tool", Content: gai.Str("tool call")},
						{BlockType: "content", Content: gai.Str("World")},
					},
				},
			},
			expected: 2, // Only content blocks should remain after filtering
		},
		{
			name:         "filters all blocks when whitelist is empty",
			allowedTypes: []string{},
			inputDialog: gai.Dialog{
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
		{
			name:         "allows multiple block types",
			allowedTypes: []string{"content", "tool"},
			inputDialog: gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: "content", Content: gai.Str("Hello")},
						{BlockType: "thinking", Content: gai.Str("Let me think about this")},
						{BlockType: "tool", Content: gai.Str("tool call")},
						{BlockType: "content", Content: gai.Str("World")},
					},
				},
			},
			expected: 3, // content and tool blocks should remain
		},
		{
			name:         "handles empty input dialog",
			allowedTypes: []string{"content"},
			inputDialog:  gai.Dialog{},
			expected:     0, // No messages in empty dialog
		},
		{
			name:         "handles message with no blocks",
			allowedTypes: []string{"content"},
			inputDialog: gai.Dialog{
				{
					Role:   gai.Assistant,
					Blocks: []gai.Block{},
				},
			},
			expected: 0, // No blocks in message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock generator that captures and returns the filtered input
			mockGenerator := &mockToolGenerator{}

			// Create the filter
			filter := NewBlockWhitelistFilter(mockGenerator, tt.allowedTypes)

			// Generate - this will filter the input dialog and pass it to the mock generator
			_, err := filter.Generate(context.Background(), tt.inputDialog, func(d gai.Dialog) *gai.GenOpts {
				return &gai.GenOpts{}
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// For empty input dialog, verify that empty dialog was passed to generator
			if len(tt.inputDialog) == 0 {
				if len(mockGenerator.capturedDialog) != 0 {
					t.Errorf("expected empty captured dialog for empty input, got %d messages", len(mockGenerator.capturedDialog))
				}
				return // Skip further checks for empty dialog case
			}

			if len(mockGenerator.capturedDialog[0].Blocks) != tt.expected {
				t.Errorf("expected %d blocks in filtered input, got %d", tt.expected, len(mockGenerator.capturedDialog[0].Blocks))
			}
		})
	}
}

// mockToolGenerator is a mock implementation of ToolGenerator for testing
type mockToolGenerator struct {
	response       gai.Dialog // Used when not capturing input
	capturedDialog gai.Dialog // Captured input dialog when captureInput is true
}

func (m *mockToolGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	m.capturedDialog = dialog
	return nil, nil
}
