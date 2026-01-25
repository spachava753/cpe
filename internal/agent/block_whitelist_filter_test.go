package agent

import (
	"context"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

func TestBlockWhitelistFilter(t *testing.T) {
	tests := []struct {
		name         string
		allowedTypes []string
		inputDialog  gai.Dialog
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
		},
		{
			name:         "handles empty input dialog",
			allowedTypes: []string{"content"},
			inputDialog:  gai.Dialog{},
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

			cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
		})
	}
}

// mockToolGenerator is a mock implementation of ToolGenerator for testing
type mockToolGenerator struct {
	capturedDialog gai.Dialog // Captured input dialog
}

func (m *mockToolGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	m.capturedDialog = dialog
	return nil, nil
}
