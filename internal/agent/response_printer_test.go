package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"
)

// mockRenderer implements Renderer for testing
type mockRenderer struct {
	renderFunc func(string) (string, error)
}

func (m *mockRenderer) Render(in string) (string, error) {
	return m.renderFunc(in)
}

func TestRenderToolCall(t *testing.T) {
	// Mock renderers that pass through input with a prefix to identify which renderer was used
	contentRenderer := &mockRenderer{
		renderFunc: func(in string) (string, error) {
			return in, nil
		},
	}
	toolCallRenderer := &mockRenderer{
		renderFunc: func(in string) (string, error) {
			return in, nil
		},
	}

	g := &ResponsePrinterGenerator{
		contentRenderer:  contentRenderer,
		toolCallRenderer: toolCallRenderer,
	}

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "execute_go_code renders as Go block",
			content: `{"name":"execute_go_code","parameters":{"code":"package main\n\nfunc Run() error { return nil }","executionTimeout":30}}`,
		},
		{
			name:    "execute_go_code without timeout renders without timeout",
			content: `{"name":"execute_go_code","parameters":{"code":"package main\n\nfunc Run() error { return nil }"}}`,
		},
		{
			name:    "regular tool renders as JSON block",
			content: `{"name":"get_weather","parameters":{"city":"New York"}}`,
		},
		{
			name:    "execute_go_code with empty code falls back to JSON",
			content: `{"name":"execute_go_code","parameters":{"code":"","executionTimeout":30}}`,
		},
		{
			name:    "execute_go_code with missing code falls back to JSON",
			content: `{"name":"execute_go_code","parameters":{"executionTimeout":30}}`,
		},
		{
			name:    "execute_go_code with non-string code falls back to JSON",
			content: `{"name":"execute_go_code","parameters":{"code":123,"executionTimeout":30}}`,
		},
		{
			name:    "malformed JSON falls back to plain text",
			content: `{invalid json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.renderToolCall(tt.content)
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestRenderToolCallUsesCorrectRenderer(t *testing.T) {
	tests := []struct {
		name                string
		content             string
		wantContentRenderer bool
	}{
		{
			name:                "execute_go_code uses contentRenderer",
			content:             `{"name":"execute_go_code","parameters":{"code":"package main","executionTimeout":30}}`,
			wantContentRenderer: true,
		},
		{
			name:                "regular tool uses toolCallRenderer",
			content:             `{"name":"get_weather","parameters":{"city":"NYC"}}`,
			wantContentRenderer: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var contentRendererCalled, toolCallRendererCalled bool

			g := &ResponsePrinterGenerator{
				contentRenderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						contentRendererCalled = true
						return in, nil
					},
				},
				toolCallRenderer: &mockRenderer{
					renderFunc: func(in string) (string, error) {
						toolCallRendererCalled = true
						return in, nil
					},
				},
			}

			g.renderToolCall(tt.content)

			if tt.wantContentRenderer {
				if !contentRendererCalled {
					t.Error("expected contentRenderer to be called, but it was not")
				}
				if toolCallRendererCalled {
					t.Error("expected toolCallRenderer to not be called, but it was")
				}
			} else {
				if contentRendererCalled {
					t.Error("expected contentRenderer to not be called, but it was")
				}
				if !toolCallRendererCalled {
					t.Error("expected toolCallRenderer to be called, but it was not")
				}
			}
		})
	}
}

func TestPlainTextRenderer(t *testing.T) {
	renderer := &PlainTextRenderer{}
	input := "**bold** and *italic*"
	output, err := renderer.Render(input)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	cupaloy.SnapshotT(t, output)
}

func TestFindLastContentBlockIndex(t *testing.T) {
	tests := []struct {
		name           string
		blocks         []blockContent
		expectedIdx    int
		expectedStderr int // count of blocks that should go to stderr
	}{
		{
			name: "single content block goes to stdout",
			blocks: []blockContent{
				{blockType: gai.Content, content: "Hello"},
			},
			expectedIdx:    0,
			expectedStderr: 0,
		},
		{
			name: "toolcall then content - only last content to stdout",
			blocks: []blockContent{
				{blockType: gai.ToolCall, content: "tool call"},
				{blockType: gai.Content, content: "response"},
			},
			expectedIdx:    1,
			expectedStderr: 1,
		},
		{
			name: "multiple content blocks - only last to stdout",
			blocks: []blockContent{
				{blockType: gai.Content, content: "first"},
				{blockType: gai.Content, content: "second"},
				{blockType: gai.Content, content: "third"},
			},
			expectedIdx:    2,
			expectedStderr: 2,
		},
		{
			name: "thinking then content - only last content to stdout",
			blocks: []blockContent{
				{blockType: gai.Thinking, content: "thinking..."},
				{blockType: gai.Content, content: "answer"},
			},
			expectedIdx:    1,
			expectedStderr: 1,
		},
		{
			name: "content then thinking - content is last content block so goes to stdout",
			blocks: []blockContent{
				{blockType: gai.Content, content: "answer"},
				{blockType: gai.Thinking, content: "thinking..."},
			},
			expectedIdx:    0,
			expectedStderr: 1,
		},
		{
			name: "only toolcalls - nothing to stdout",
			blocks: []blockContent{
				{blockType: gai.ToolCall, content: "tool1"},
				{blockType: gai.ToolCall, content: "tool2"},
			},
			expectedIdx:    -1,
			expectedStderr: 2,
		},
		{
			name:           "empty blocks - nothing to stdout",
			blocks:         []blockContent{},
			expectedIdx:    -1,
			expectedStderr: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find last content index using the same algorithm as Generate
			var lastContentIdx = -1
			for i := len(tt.blocks) - 1; i >= 0; i-- {
				if tt.blocks[i].blockType == gai.Content {
					lastContentIdx = i
					break
				}
			}

			if lastContentIdx != tt.expectedIdx {
				t.Errorf("expected stdout index %d, got %d", tt.expectedIdx, lastContentIdx)
			}

			stderrCount := 0
			for i := range tt.blocks {
				if i != lastContentIdx || lastContentIdx == -1 {
					stderrCount++
				}
			}
			// Adjust for the case when lastContentIdx is -1 (no stdout output)
			if lastContentIdx == -1 {
				stderrCount = len(tt.blocks)
			}

			if stderrCount != tt.expectedStderr {
				t.Errorf("expected %d blocks to stderr, got %d", tt.expectedStderr, stderrCount)
			}
		})
	}
}

// mockGenerator implements gai.ToolCapableGenerator for testing
type mockGenerator struct {
	response gai.Response
	err      error
}

func (m *mockGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	return m.response, m.err
}

func (m *mockGenerator) Register(tool gai.Tool) error {
	return nil
}

// ioRoutingResult captures both stdout and stderr for snapshot comparison
type ioRoutingResult struct {
	Stdout string
	Stderr string
}

func TestResponsePrinterGenerateIORouting(t *testing.T) {
	tests := []struct {
		name   string
		blocks []gai.Block
	}{
		{
			name: "single content block goes to stdout",
			blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Hello world")},
			},
		},
		{
			name: "thinking then content - thinking to stderr, content to stdout",
			blocks: []gai.Block{
				{BlockType: gai.Thinking, ModalityType: gai.Text, Content: gai.Str("Let me think...")},
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("The answer is 42")},
			},
		},
		{
			name: "multiple content blocks - only last to stdout",
			blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("First part")},
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Second part")},
			},
		},
		{
			name:   "empty response - nothing to either stream",
			blocks: []gai.Block{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create pipes to capture output
			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()

			// Create a generator with plain text renderers for predictable output
			plainRenderer := &PlainTextRenderer{}
			mockGen := &mockGenerator{
				response: gai.Response{
					Candidates: []gai.Message{
						{Blocks: tt.blocks},
					},
				},
			}

			gen := &ResponsePrinterGenerator{
				GeneratorWrapper: gai.GeneratorWrapper{Inner: mockGen},
				contentRenderer:  plainRenderer,
				thinkingRenderer: plainRenderer,
				toolCallRenderer: plainRenderer,
				stdout:           wOut,
				stderr:           wErr,
			}

			// Run Generate
			_, _ = gen.Generate(context.Background(), nil, nil)

			// Close writers and restore
			wOut.Close()
			wErr.Close()

			// Read captured output
			var stdoutBuf, stderrBuf bytes.Buffer
			io.Copy(&stdoutBuf, rOut)
			io.Copy(&stderrBuf, rErr)

			result := ioRoutingResult{
				Stdout: strings.TrimSpace(stdoutBuf.String()),
				Stderr: strings.TrimSpace(stderrBuf.String()),
			}

			cupaloy.SnapshotT(t, result)
		})
	}
}
