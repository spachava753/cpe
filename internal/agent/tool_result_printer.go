package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/types"
)

const maxLines = 20

// ToolResultPrinterWrapper wraps a Generator and prints tool execution results
// when Generate is called with a dialog containing a tool result as the last message.
type ToolResultPrinterWrapper struct {
	gai.GeneratorWrapper
	renderer types.Renderer
	output   io.Writer
}

// NewToolResultPrinterWrapper creates a new ToolResultPrinterWrapper that writes to stderr.
func NewToolResultPrinterWrapper(g gai.Generator, renderer types.Renderer) *ToolResultPrinterWrapper {
	return &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
		renderer:         renderer,
		output:           os.Stderr,
	}
}

// WithToolResultPrinterWrapper returns a WrapperFunc for use with gai.Wrap.
// Output is written to stderr.
func WithToolResultPrinterWrapper(renderer types.Renderer) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return &ToolResultPrinterWrapper{
			GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
			renderer:         renderer,
			output:           os.Stderr,
		}
	}
}

// Generate checks if the last message is a tool result and prints it before delegating.
func (g *ToolResultPrinterWrapper) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	if len(dialog) > 0 {
		lastMsg := dialog[len(dialog)-1]
		if lastMsg.Role == gai.ToolResult {
			g.printToolResult(dialog, lastMsg)
		}
	}
	return g.GeneratorWrapper.Generate(ctx, dialog, opts)
}

// printToolResult prints the tool result block from the message.
// The gai package ensures there is exactly one tool result block per message.
func (g *ToolResultPrinterWrapper) printToolResult(dialog gai.Dialog, toolResultMsg gai.Message) {
	// Find the tool name by looking at the previous assistant message's tool calls
	toolName := g.findToolName(dialog, toolResultMsg)

	// Get message ID if available
	messageID := GetMessageID(toolResultMsg)

	// Print the first block (gai ensures single tool result per message)
	if len(toolResultMsg.Blocks) > 0 {
		block := toolResultMsg.Blocks[0]
		g.printResult(toolName, block, messageID)
	}
}

// findToolName looks up the tool name from the previous assistant message by matching tool call ID.
func (g *ToolResultPrinterWrapper) findToolName(dialog gai.Dialog, toolResultMsg gai.Message) string {
	if len(toolResultMsg.Blocks) == 0 {
		return "unknown"
	}
	toolCallID := toolResultMsg.Blocks[0].ID

	// Look at the previous assistant message to find the matching tool call
	if len(dialog) >= 2 {
		prevMsg := dialog[len(dialog)-2]
		if prevMsg.Role == gai.Assistant {
			for _, block := range prevMsg.Blocks {
				if block.BlockType == gai.ToolCall && block.ID == toolCallID {
					var toolCall gai.ToolCallInput
					if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err == nil {
						return toolCall.Name
					}
				}
			}
		}
	}
	return "unknown"
}

// printResult prints a tool result block to the output with truncation and rendering.
func (g *ToolResultPrinterWrapper) printResult(toolName string, block gai.Block, messageID string) {
	var markdownContent string

	// Handle non-text modalities by just showing the mimetype
	if block.ModalityType != gai.Text {
		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = block.ModalityType.String()
		}
		markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n[%s content]", toolName, mimeType)
	} else {
		contentStr := block.Content.String()
		isCodeMode := toolName == codemode.ExecuteGoCodeToolName

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
				markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n```json\n%s\n```", toolName, truncated)
			} else {
				truncated := truncateToMaxLines(contentStr, maxLines)
				markdownContent = fmt.Sprintf("#### Tool \"%s\" result:\n```\n%s\n```", toolName, truncated)
			}
		}
	}

	rendered, err := g.renderer.Render(markdownContent)
	if err != nil {
		fmt.Fprint(g.output, "\n"+markdownContent+"\n")
	} else {
		fmt.Fprint(g.output, "\n"+rendered)
	}

	// Print message ID if available (format matches token usage printer style)
	if messageID != "" {
		idMsg, _ := g.renderer.Render(fmt.Sprintf("> message_id: `%s`", messageID))
		fmt.Fprint(g.output, idMsg)
	}
}
