package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/ports"
)

const (
	maxLines        = 20
	unknownToolName = "unknown"
)

// ToolResultPrinterWrapper wraps a Generator and prints tool execution results
// when Generate is called with a dialog containing a tool result as the last message.
type ToolResultPrinterWrapper struct {
	gai.GeneratorWrapper
	renderer ports.Renderer
	output   io.Writer
}

// NewToolResultPrinterWrapper creates a new ToolResultPrinterWrapper that writes to stderr.
func NewToolResultPrinterWrapper(g gai.Generator, renderer ports.Renderer) *ToolResultPrinterWrapper {
	return &ToolResultPrinterWrapper{
		GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
		renderer:         renderer,
		output:           os.Stderr,
	}
}

// WithToolResultPrinterWrapper returns a WrapperFunc for use with gai.Wrap.
// Output is written to stderr.
func WithToolResultPrinterWrapper(renderer ports.Renderer) gai.WrapperFunc {
	return func(g gai.Generator) gai.Generator {
		return &ToolResultPrinterWrapper{
			GeneratorWrapper: gai.GeneratorWrapper{Inner: g},
			renderer:         renderer,
			output:           os.Stderr,
		}
	}
}

// Generate checks if the dialog ends with tool results and prints each result
// from the most recent assistant tool-call turn before delegating.
func (g *ToolResultPrinterWrapper) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	for _, toolResultMsg := range g.trailingToolResults(dialog) {
		g.printToolResult(dialog, toolResultMsg)
	}
	return g.GeneratorWrapper.Generate(ctx, dialog, opts)
}

func (g *ToolResultPrinterWrapper) trailingToolResults(dialog gai.Dialog) []gai.Message {
	if len(dialog) == 0 || dialog[len(dialog)-1].Role != gai.ToolResult {
		return nil
	}

	lastAssistantIdx := -1
	for i := len(dialog) - 1; i >= 0; i-- {
		if dialog[i].Role == gai.Assistant {
			lastAssistantIdx = i
			break
		}
	}
	if lastAssistantIdx < 0 {
		return nil
	}

	var results []gai.Message
	for i := lastAssistantIdx + 1; i < len(dialog); i++ {
		if dialog[i].Role == gai.ToolResult {
			results = append(results, dialog[i])
		}
	}
	return results
}

// printToolResult prints all tool result blocks from the message.
func (g *ToolResultPrinterWrapper) printToolResult(dialog gai.Dialog, toolResultMsg gai.Message) {
	toolName := g.findToolName(dialog, toolResultMsg)
	messageID := GetMessageID(toolResultMsg)
	if len(toolResultMsg.Blocks) == 0 {
		return
	}
	g.printResult(toolName, toolResultMsg.Blocks, messageID)
}

// findToolName looks up the tool name by matching the tool call ID against the
// nearest preceding assistant message. This handles multiple consecutive tool
// results from a single assistant turn without accidentally reusing stale tool
// calls from older turns.
func (g *ToolResultPrinterWrapper) findToolName(dialog gai.Dialog, toolResultMsg gai.Message) string {
	if len(toolResultMsg.Blocks) == 0 {
		return unknownToolName
	}
	toolCallID := toolResultMsg.Blocks[0].ID
	if toolCallID == "" {
		return unknownToolName
	}

	for i := len(dialog) - 2; i >= 0; i-- {
		msg := dialog[i]
		if msg.Role != gai.Assistant {
			continue
		}
		for _, block := range msg.Blocks {
			if block.BlockType != gai.ToolCall || block.ID != toolCallID {
				continue
			}
			var toolCall gai.ToolCallInput
			if err := json.Unmarshal([]byte(block.Content.String()), &toolCall); err == nil && toolCall.Name != "" {
				return toolCall.Name
			}
			return unknownToolName
		}
		return unknownToolName
	}
	return unknownToolName
}

// printResult prints tool result blocks to the output with truncation and rendering.
func (g *ToolResultPrinterWrapper) printResult(toolName string, blocks []gai.Block, messageID string) {
	sections := []string{fmt.Sprintf("#### Tool \"%s\" result:", toolName)}
	for _, block := range blocks {
		sections = append(sections, formatToolResultBlockMarkdown(toolName, block))
	}
	markdownContent := strings.Join(sections, "\n\n")

	rendered, err := g.renderer.Render(markdownContent)
	if err != nil {
		fmt.Fprint(g.output, "\n"+markdownContent+"\n")
	} else {
		fmt.Fprint(g.output, "\n"+rendered)
	}

	if messageID != "" {
		idMsg, _ := g.renderer.Render(fmt.Sprintf("> message_id: `%s`", messageID))
		fmt.Fprint(g.output, idMsg)
	}
}

func formatToolResultBlockMarkdown(toolName string, block gai.Block) string {
	if block.ModalityType != gai.Text {
		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = block.ModalityType.String()
		}
		return fmt.Sprintf("[%s content]", mimeType)
	}

	contentStr := block.Content.String()
	if toolName == codemode.ExecuteGoCodeToolName {
		return codemode.FormatResultMarkdown(contentStr, maxLines)
	}

	var jsonData interface{}
	if err := json.Unmarshal([]byte(contentStr), &jsonData); err == nil {
		formatted, err := json.MarshalIndent(jsonData, "", "  ")
		if err == nil {
			contentStr = string(formatted)
		}
		truncated := truncateToolResultToMaxLines(contentStr, maxLines)
		return fmt.Sprintf("```json\n%s\n```", truncated)
	}

	truncated := truncateToolResultToMaxLines(contentStr, maxLines)
	return fmt.Sprintf("```\n%s\n```", truncated)
}

func truncateToolResultToMaxLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return content
	}
	if content == "" {
		return content
	}

	trailingNewline := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimSuffix(content, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= maxLines {
		return content
	}

	truncated := strings.Join(lines[:maxLines], "\n")
	if trailingNewline {
		truncated += "\n"
	}
	return truncated + "... (truncated)"
}
