package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/types"
)

const maxLines = 20

// ToolCallbackPrinter wraps a ToolCallback and prints its results to stderr
type ToolCallbackPrinter struct {
	wrapped  gai.ToolCallback
	toolName string
	renderer types.Renderer
}

// Call executes the wrapped callback and prints the result
func (p *ToolCallbackPrinter) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	msg, err := p.wrapped.Call(ctx, parametersJSON, toolCallID)
	if err != nil {
		return msg, err
	}
	p.printResult(msg)
	return msg, nil
}

// printResult prints the tool result to stderr with truncation and rendering
func (p *ToolCallbackPrinter) printResult(msg gai.Message) {
	var output strings.Builder

	isCodeMode := p.toolName == codemode.ExecuteGoCodeToolName

	var content strings.Builder
	for _, block := range msg.Blocks {
		if block.ModalityType == gai.Text {
			content.WriteString(block.Content.String())
		}
	}

	contentStr := content.String()

	var markdownContent string
	if isCodeMode {
		markdownContent = FormatExecuteGoCodeResultMarkdown(contentStr, maxLines)
	} else {
		var jsonData interface{}
		if err := json.Unmarshal([]byte(contentStr), &jsonData); err == nil {
			formatted, err := json.MarshalIndent(jsonData, "", "  ")
			if err == nil {
				contentStr = string(formatted)
			}
			truncated := truncateToMaxLines(contentStr, maxLines)
			markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n```json\n%s\n```", p.toolName, truncated)
		} else {
			truncated := truncateToMaxLines(contentStr, maxLines)
			markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n```\n%s\n```", p.toolName, truncated)
		}
	}

	rendered, err := p.renderer.Render(markdownContent)
	if err != nil {
		output.WriteString("\n")
		output.WriteString(markdownContent)
		output.WriteString("\n")
	} else {
		output.WriteString("\n")
		output.WriteString(rendered)
	}

	fmt.Fprint(os.Stderr, output.String())
}

// ToolResultPrinterWrapper wraps a types.Generator (typically gai.ToolGenerator)
// and prints tool execution results by wrapping callbacks.
type ToolResultPrinterWrapper struct {
	gai.GeneratorWrapper
	renderer types.Renderer
}

// NewToolResultPrinterWrapper creates a new ToolResultPrinterWrapper
func NewToolResultPrinterWrapper(g gai.Generator, renderer types.Renderer) *ToolResultPrinterWrapper {
	return &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
		renderer: renderer,
	}
}

// WithToolResultPrinterWrapper returns a WrapperFunc for use with gai.Wrap
func WithToolResultPrinterWrapper(renderer types.Renderer) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return &ToolResultPrinterWrapper{
			GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
			renderer: renderer,
		}
	}
}

// Register wraps the callback with a printer and registers it with the wrapped generator.
// If callback is nil, it is passed through without wrapping.
func (g *ToolResultPrinterWrapper) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if callback == nil {
		return g.Inner.(types.ToolRegistrar).Register(tool, nil)
	}
	wrappedCallback := &ToolCallbackPrinter{
		wrapped:  callback,
		toolName: tool.Name,
		renderer: g.renderer,
	}
	return g.Inner.(types.ToolRegistrar).Register(tool, wrappedCallback)
}
