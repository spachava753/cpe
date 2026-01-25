package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

const maxLines = 20

// ToolCallbackPrinter wraps a ToolCallback and prints its results to stderr
type ToolCallbackPrinter struct {
	wrapped  gai.ToolCallback
	toolName string
	renderer Renderer
}

// Call executes the wrapped callback and prints the result
func (p *ToolCallbackPrinter) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	// Execute the wrapped callback
	msg, err := p.wrapped.Call(ctx, parametersJSON, toolCallID)
	if err != nil {
		return msg, err
	}

	// Print the result
	p.printResult(msg)

	return msg, nil
}

// printResult prints the tool result to stderr with truncation and rendering
func (p *ToolCallbackPrinter) printResult(msg gai.Message) {
	var output strings.Builder

	// Determine if this is execute_go_code or a regular tool
	isCodeMode := p.toolName == codemode.ExecuteGoCodeToolName

	// Collect all text content from blocks
	var content strings.Builder
	for _, block := range msg.Blocks {
		if block.ModalityType == gai.Text {
			content.WriteString(block.Content.String())
		}
	}

	contentStr := content.String()

	// Build markdown content based on tool type
	var markdownContent string
	if isCodeMode {
		// Use shared formatting function for code mode
		markdownContent = FormatExecuteGoCodeResultMarkdown(contentStr, maxLines)
	} else {
		// For regular tools, try to format as JSON
		var jsonData interface{}
		if err := json.Unmarshal([]byte(contentStr), &jsonData); err == nil {
			// Valid JSON, pretty print it
			formatted, err := json.MarshalIndent(jsonData, "", "  ")
			if err == nil {
				contentStr = string(formatted)
			}
			truncated := truncateToMaxLines(contentStr, maxLines)
			markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n```json\n%s\n```", p.toolName, truncated)
		} else {
			// Not JSON, treat as plain text
			truncated := truncateToMaxLines(contentStr, maxLines)
			markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n```\n%s\n```", p.toolName, truncated)
		}
	}

	// Render with glamour
	rendered, err := p.renderer.Render(markdownContent)
	if err != nil {
		// Fallback to plain text if rendering fails
		output.WriteString("\n")
		output.WriteString(markdownContent)
		output.WriteString("\n")
	} else {
		output.WriteString("\n")
		output.WriteString(rendered)
	}

	// Write to stderr
	fmt.Fprint(os.Stderr, output.String())
}

// ToolResultPrinterWrapper wraps an Iface and prints tool execution results
type ToolResultPrinterWrapper struct {
	wrapped  Iface
	renderer Renderer
}

// NewToolResultPrinterWrapper creates a new ToolResultPrinterWrapper with a glamour renderer
func NewToolResultPrinterWrapper(wrapped Iface, renderer Renderer) *ToolResultPrinterWrapper {
	return &ToolResultPrinterWrapper{
		wrapped:  wrapped,
		renderer: renderer,
	}
}

// Generate delegates to the wrapped generator
func (g *ToolResultPrinterWrapper) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	return g.wrapped.Generate(ctx, dialog, optsGen)
}

// Register wraps the callback with a printer and registers it with the wrapped generator.
// If callback is nil, it is passed through without wrapping (nil callbacks terminate
// execution immediately in gai.ToolGenerator).
func (g *ToolResultPrinterWrapper) Register(tool gai.Tool, callback gai.ToolCallback) error {
	// Register with the wrapped generator using the local ToolRegister interface
	if toolRegister, ok := g.wrapped.(ToolRegister); !ok {
		return gai.ToolRegistrationErr{Tool: tool.Name, Cause: fmt.Errorf("underlying generator does not support tool registration")}
	} else if callback == nil {
		// Pass through nil callbacks without wrapping
		return toolRegister.Register(tool, nil)
	} else {
		// Wrap the callback with our printer
		wrappedCallback := &ToolCallbackPrinter{
			wrapped:  callback,
			toolName: tool.Name,
			renderer: g.renderer,
		}
		return toolRegister.Register(tool, wrappedCallback)
	}
}

// Inner returns the wrapped generator, implementing the InnerGenerator interface
func (g *ToolResultPrinterWrapper) Inner() Iface {
	return g.wrapped
}
