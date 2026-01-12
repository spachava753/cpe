package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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
		validateContent  func(t *testing.T, content string)
	}{
		{
			name:             "prints JSON for regular tool",
			toolName:         "get_weather",
			callbackResponse: `{"temperature": 72, "condition": "sunny"}`,
			expectedBlocks:   1,
			validateContent: func(t *testing.T, content string) {
				if content != `{"temperature": 72, "condition": "sunny"}` {
					t.Errorf("unexpected content: %s", content)
				}
			},
		},
		{
			name:             "prints text for code mode",
			toolName:         codemode.ExecuteGoCodeToolName,
			callbackResponse: "The weather is sunny and 72 degrees",
			expectedBlocks:   1,
			validateContent: func(t *testing.T, content string) {
				if content != "The weather is sunny and 72 degrees" {
					t.Errorf("unexpected content: %s", content)
				}
			},
		},
		{
			name:             "truncates long output",
			toolName:         "test_tool",
			callbackResponse: strings.Join(makeLines(25), "\n"),
			expectedBlocks:   1,
			validateContent: func(t *testing.T, content string) {
				expected := strings.Join(makeLines(25), "\n")
				if content != expected {
					t.Errorf("message content was modified, should not be")
				}
			},
		},
		{
			name:             "handles empty response",
			toolName:         "empty_tool",
			callbackResponse: "",
			expectedBlocks:   1,
			validateContent: func(t *testing.T, content string) {
				if content != "" {
					t.Errorf("expected empty content, got: %s", content)
				}
			},
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

			if tt.validateContent != nil {
				tt.validateContent(t, msg.Blocks[0].Content.String())
			}
		})
	}
}

func TestTruncateToLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
		want     string
	}{
		{
			name:     "content shorter than max",
			content:  "line1\nline2\nline3",
			maxLines: 5,
			want:     "line1\nline2\nline3",
		},
		{
			name:     "content exactly at max",
			content:  "line1\nline2\nline3",
			maxLines: 3,
			want:     "line1\nline2\nline3",
		},
		{
			name:     "content longer than max",
			content:  "line1\nline2\nline3\nline4\nline5",
			maxLines: 3,
			want:     "line1\nline2\nline3\n... (truncated)",
		},
		{
			name:     "single line",
			content:  "single line",
			maxLines: 20,
			want:     "single line",
		},
		{
			name:     "empty content",
			content:  "",
			maxLines: 20,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToLines(tt.content, tt.maxLines)
			if got != tt.want {
				t.Errorf("truncateToLines() = %q, want %q", got, tt.want)
			}
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
