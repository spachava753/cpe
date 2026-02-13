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
			mockGenerator := &mockGaiGenerator{}
			filter := NewBlockWhitelistFilter(mockGenerator, tt.allowedTypes)

			_, err := filter.Generate(context.Background(), tt.inputDialog, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cupaloy.SnapshotT(t, mockGenerator.capturedDialog)
		})
	}
}

func TestBlockWhitelistFilter_PreservesExtraFields(t *testing.T) {
	mockGenerator := &mockGaiGenerator{}
	filter := NewBlockWhitelistFilter(mockGenerator, []string{gai.Content, gai.ToolCall})

	inputDialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, Content: gai.Str("hello")},
			},
			ExtraFields: map[string]interface{}{"cpe_message_id": "msg_abc123"},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{BlockType: gai.Thinking, Content: gai.Str("thinking...")},
				{BlockType: gai.Content, Content: gai.Str("response")},
			},
			ExtraFields: map[string]interface{}{"cpe_message_id": "msg_def456"},
		},
	}

	_, err := filter.Generate(context.Background(), inputDialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, msg := range mockGenerator.capturedDialog {
		if msg.ExtraFields == nil {
			t.Errorf("message %d: ExtraFields is nil, expected it to be preserved", i)
			continue
		}
		id, ok := msg.ExtraFields["cpe_message_id"].(string)
		if !ok || id == "" {
			t.Errorf("message %d: expected cpe_message_id to be preserved, got %v", i, msg.ExtraFields)
		}
	}

	if id := mockGenerator.capturedDialog[0].ExtraFields["cpe_message_id"]; id != "msg_abc123" {
		t.Errorf("message 0: expected cpe_message_id 'msg_abc123', got %v", id)
	}
	if id := mockGenerator.capturedDialog[1].ExtraFields["cpe_message_id"]; id != "msg_def456" {
		t.Errorf("message 1: expected cpe_message_id 'msg_def456', got %v", id)
	}
}

// mockGaiGenerator implements gai.Generator for testing filters at the gai.Generator level
type mockGaiGenerator struct {
	capturedDialog gai.Dialog
}

func (m *mockGaiGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	m.capturedDialog = dialog
	return gai.Response{}, nil
}
