package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

// toolResultMockGen is a mock implementation of gai.Generator for testing
type toolResultMockGen struct {
	response gai.Response
	err      error
}

func (m *toolResultMockGen) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	return m.response, m.err
}

// errorRenderer always returns an error
type errorRenderer struct{}

func (e *errorRenderer) Render(content string) (string, error) {
	return "", errors.New("render error")
}

func TestToolResultPrinterWrapper_PrintsToolResults(t *testing.T) {
	tests := []struct {
		name           string
		toolName       string
		toolResultText string
	}{
		{
			name:           "prints JSON for regular tool",
			toolName:       "get_weather",
			toolResultText: `{"temperature": 72, "condition": "sunny"}`,
		},
		{
			name:           "prints text for code mode",
			toolName:       codemode.ExecuteGoCodeToolName,
			toolResultText: "The weather is sunny and 72 degrees",
		},
		{
			name:           "truncates long output",
			toolName:       "test_tool",
			toolResultText: strings.Join(makeLines(25), "\n"),
		},
		{
			name:           "handles empty response",
			toolName:       "empty_tool",
			toolResultText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGen := &toolResultMockGen{
				response: gai.Response{
					Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
				},
			}

			var output bytes.Buffer
			wrapper := &ToolResultPrinterWrapper{
				GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
				renderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						return in, nil
					},
				},
				output: &output,
			}

			// Create tool call JSON
			toolCallJSON, _ := json.Marshal(gai.ToolCallInput{
				Name:       tt.toolName,
				Parameters: map[string]any{},
			})

			// Build dialog with assistant tool call followed by tool result
			dialog := gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							ID:           "call-1",
							BlockType:    gai.ToolCall,
							ModalityType: gai.Text,
							Content:      gai.Str(string(toolCallJSON)),
						},
					},
				},
				{
					Role: gai.ToolResult,
					Blocks: []gai.Block{
						{
							ID:           "call-1",
							BlockType:    gai.Content,
							ModalityType: gai.Text,
							MimeType:     "text/plain",
							Content:      gai.Str(tt.toolResultText),
						},
					},
				},
			}

			_, err := wrapper.Generate(context.Background(), dialog, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cupaloy.SnapshotT(t, output.String())
		})
	}
}

func TestToolResultPrinterWrapper_DoesNotPrintForNonToolResult(t *testing.T) {
	mockGen := &toolResultMockGen{
		response: gai.Response{
			Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
		},
	}

	var output bytes.Buffer
	wrapper := &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
		renderer: &mockRenderer{
			renderFunc: func(in string) (string, error) {
				return in, nil
			},
		},
		output: &output,
	}

	// Dialog with just a user message (no tool result)
	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}},
	}

	_, err := wrapper.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Len() > 0 {
		t.Errorf("expected no output for non-tool-result dialog, got: %s", output.String())
	}
}

func TestToolResultPrinterWrapper_UnknownToolName(t *testing.T) {
	mockGen := &toolResultMockGen{
		response: gai.Response{
			Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
		},
	}

	var output bytes.Buffer
	wrapper := &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
		renderer: &mockRenderer{
			renderFunc: func(in string) (string, error) {
				return in, nil
			},
		},
		output: &output,
	}

	// Tool call has ID "call-1", but tool result has mismatched ID "call-999"
	toolCallJSON, _ := json.Marshal(gai.ToolCallInput{
		Name:       "some_tool",
		Parameters: map[string]any{},
	})

	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					ID:           "call-1",
					BlockType:    gai.ToolCall,
					ModalityType: gai.Text,
					Content:      gai.Str(string(toolCallJSON)),
				},
			},
		},
		{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           "call-999", // Mismatched ID
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					Content:      gai.Str("result"),
				},
			},
		},
	}

	_, err := wrapper.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cupaloy.SnapshotT(t, output.String())
}

func TestToolResultPrinterWrapper_RendererError(t *testing.T) {
	mockGen := &toolResultMockGen{
		response: gai.Response{
			Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
		},
	}

	var output bytes.Buffer
	wrapper := &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
		renderer:         &errorRenderer{},
		output:           &output,
	}

	toolCallJSON, _ := json.Marshal(gai.ToolCallInput{
		Name:       "test_tool",
		Parameters: map[string]any{},
	})

	dialog := gai.Dialog{
		{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{
					ID:           "call-1",
					BlockType:    gai.ToolCall,
					ModalityType: gai.Text,
					Content:      gai.Str(string(toolCallJSON)),
				},
			},
		},
		{
			Role: gai.ToolResult,
			Blocks: []gai.Block{
				{
					ID:           "call-1",
					BlockType:    gai.Content,
					ModalityType: gai.Text,
					Content:      gai.Str("tool output"),
				},
			},
		},
	}

	_, err := wrapper.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cupaloy.SnapshotT(t, output.String())
}

func TestToolResultPrinterWrapper_EmptyDialog(t *testing.T) {
	mockGen := &toolResultMockGen{
		response: gai.Response{
			Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
		},
	}

	var output bytes.Buffer
	wrapper := &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
		renderer: &mockRenderer{
			renderFunc: func(in string) (string, error) {
				return in, nil
			},
		},
		output: &output,
	}

	// Empty dialog should not panic
	_, err := wrapper.Generate(context.Background(), gai.Dialog{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Len() > 0 {
		t.Errorf("expected no output for empty dialog, got: %s", output.String())
	}
}

func TestToolResultPrinterWrapper_MultimediaContent(t *testing.T) {
	tests := []struct {
		name         string
		modalityType gai.Modality
		mimeType     string
	}{
		{
			name:         "image content shows mimetype",
			modalityType: gai.Image,
			mimeType:     "image/png",
		},
		{
			name:         "audio content shows mimetype",
			modalityType: gai.Audio,
			mimeType:     "audio/wav",
		},
		{
			name:         "video content shows mimetype",
			modalityType: gai.Video,
			mimeType:     "video/mp4",
		},
		{
			name:         "non-text with empty mimetype uses modality string",
			modalityType: gai.Image,
			mimeType:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGen := &toolResultMockGen{
				response: gai.Response{
					Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
				},
			}

			var output bytes.Buffer
			wrapper := &ToolResultPrinterWrapper{
				GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
				renderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						return in, nil
					},
				},
				output: &output,
			}

			toolCallJSON, _ := json.Marshal(gai.ToolCallInput{
				Name:       "image_tool",
				Parameters: map[string]any{},
			})

			dialog := gai.Dialog{
				{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("test")}},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							ID:           "call-1",
							BlockType:    gai.ToolCall,
							ModalityType: gai.Text,
							Content:      gai.Str(string(toolCallJSON)),
						},
					},
				},
				{
					Role: gai.ToolResult,
					Blocks: []gai.Block{
						{
							ID:           "call-1",
							BlockType:    gai.Content,
							ModalityType: tt.modalityType,
							MimeType:     tt.mimeType,
							Content:      gai.Str("base64encodeddata"),
						},
					},
				},
			}

			_, err := wrapper.Generate(context.Background(), dialog, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cupaloy.SnapshotT(t, output.String())
		})
	}
}

func TestWithToolResultPrinterWrapper(t *testing.T) {
	mockGen := &toolResultMockGen{
		response: gai.Response{
			Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("done")}}},
		},
	}

	renderer := &mockRenderer{
		renderFunc: func(in string) (string, error) {
			return in, nil
		},
	}

	wrapperFunc := WithToolResultPrinterWrapper(renderer)
	wrapped := wrapperFunc(mockGen)

	// Verify the wrapper was created correctly
	printerWrapper, ok := wrapped.(*ToolResultPrinterWrapper)
	if !ok {
		t.Fatalf("expected *ToolResultPrinterWrapper, got %T", wrapped)
	}

	if printerWrapper.renderer != renderer {
		t.Error("renderer not set correctly")
	}

	if printerWrapper.output == nil {
		t.Error("output should default to non-nil (stderr)")
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
