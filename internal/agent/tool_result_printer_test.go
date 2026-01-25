package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

// mockToolCallback is a mock implementation of gai.ToolCallback for testing
type mockToolCallback struct {
	response string
}

func (m *mockToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	return gai.Message{
		Role: gai.ToolResult,
		Blocks: []gai.Block{
			{
				ID:           toolCallID,
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str(m.response),
			},
		},
	}, nil
}

func TestToolCallbackPrinter(t *testing.T) {
	tests := []struct {
		name             string
		toolName         string
		callbackResponse string
		expectedBlocks   int
	}{
		{
			name:             "prints JSON for regular tool",
			toolName:         "get_weather",
			callbackResponse: `{"temperature": 72, "condition": "sunny"}`,
			expectedBlocks:   1,
		},
		{
			name:             "prints text for code mode",
			toolName:         codemode.ExecuteGoCodeToolName,
			callbackResponse: "The weather is sunny and 72 degrees",
			expectedBlocks:   1,
		},
		{
			name:             "truncates long output",
			toolName:         "test_tool",
			callbackResponse: strings.Join(makeLines(25), "\n"),
			expectedBlocks:   1,
		},
		{
			name:             "handles empty response",
			toolName:         "empty_tool",
			callbackResponse: "",
			expectedBlocks:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCallback := &mockToolCallback{
				response: tt.callbackResponse,
			}

			printer := &ToolCallbackPrinter{
				wrapped:  mockCallback,
				toolName: tt.toolName,
				renderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						return in, nil
					},
				},
			}

			msg, err := printer.Call(context.Background(), json.RawMessage("{}"), "test-id")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(msg.Blocks) != tt.expectedBlocks {
				t.Fatalf("expected %d block(s), got %d", tt.expectedBlocks, len(msg.Blocks))
			}

			cupaloy.SnapshotT(t, msg.Blocks[0].Content.String())
		})
	}
}

func TestTruncateToLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
	}{
		{
			name:     "content shorter than max",
			content:  "line1\nline2\nline3",
			maxLines: 5,
		},
		{
			name:     "content exactly at max",
			content:  "line1\nline2\nline3",
			maxLines: 3,
		},
		{
			name:     "content longer than max",
			content:  "line1\nline2\nline3\nline4\nline5",
			maxLines: 3,
		},
		{
			name:     "single line",
			content:  "single line",
			maxLines: 20,
		},
		{
			name:     "empty content",
			content:  "",
			maxLines: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToMaxLines(tt.content, tt.maxLines)
			cupaloy.SnapshotT(t, got)
		})
	}
}

// makeLines generates n lines of test content
func makeLines(n int) []string {
	var lines []string
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("Line %d", i))
	}
	return lines
}
