package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

// ResponsePrinterGenerator is a wrapper around another generator that prints out
// the response returned from the wrapped generator with styled markdown rendering.
type ResponsePrinterGenerator struct {
	wrapped          gai.ToolCapableGenerator
	contentRenderer  Renderer
	thinkingRenderer Renderer
	toolCallRenderer Renderer
	stdout           io.Writer
	stderr           io.Writer
}

// NewResponsePrinterGenerator creates a new ResponsePrinterGenerator with the provided renderers and writers.
func NewResponsePrinterGenerator(
	wrapped gai.ToolCapableGenerator,
	contentRenderer Renderer,
	thinkingRenderer Renderer,
	toolCallRenderer Renderer,
	stdout io.Writer,
	stderr io.Writer,
) *ResponsePrinterGenerator {
	return &ResponsePrinterGenerator{
		wrapped:          wrapped,
		contentRenderer:  contentRenderer,
		thinkingRenderer: thinkingRenderer,
		toolCallRenderer: toolCallRenderer,
		stdout:           stdout,
		stderr:           stderr,
	}
}

func (g *ResponsePrinterGenerator) renderContent(content string) string {
	rendered, err := g.contentRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (g *ResponsePrinterGenerator) renderThinking(content string, reasoningType any) string {
	if reasoningType == "reasoning.encrypted" {
		content = "[Reasoning content is encrypted]\n"
	}
	rendered, err := g.thinkingRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return rendered
}

func (g *ResponsePrinterGenerator) renderToolCall(content string) string {
	var toolCall gai.ToolCallInput
	if err := json.Unmarshal([]byte(content), &toolCall); err == nil {
		if toolCall.Name == codemode.ExecuteGoCodeToolName {
			paramsJSON, err := json.Marshal(toolCall.Parameters)
			if err == nil {
				var input codemode.ExecuteGoCodeInput
				if err := json.Unmarshal(paramsJSON, &input); err == nil && input.Code != "" {
					header := "#### [tool call]"
					if input.ExecutionTimeout > 0 {
						header = fmt.Sprintf("#### [tool call] (timeout: %ds)", input.ExecutionTimeout)
					}
					result := fmt.Sprintf("%s\n```go\n%s\n```", header, input.Code)
					if rendered, err := g.contentRenderer.Render(result); err == nil {
						return rendered
					}
				}
			}
		}
	}

	var formattedJson bytes.Buffer
	if err := json.Indent(&formattedJson, []byte(content), "", "  "); err != nil {
		return content
	}
	result := fmt.Sprintf("#### [tool call]\n```json\n%s\n```", formattedJson.String())

	if rendered, err := g.toolCallRenderer.Render(result); err == nil {
		return rendered
	}
	return content
}

type blockContent struct {
	blockType string
	content   string
}

func (g *ResponsePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	response, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	var blocks []blockContent

	for _, candidate := range response.Candidates {
		for _, block := range candidate.Blocks {
			if block.ModalityType != gai.Text {
				blocks = append(blocks, blockContent{
					blockType: block.BlockType,
					content:   fmt.Sprintf("Received non-text block of type: %s\n", block.ModalityType),
				})
				continue
			}

			content := block.Content.String()
			switch block.BlockType {
			case gai.Content:
				content = g.renderContent(content)
			case gai.Thinking:
				content = g.renderThinking(content, block.ExtraFields["reasoning_type"])
			case gai.ToolCall:
				content = g.renderToolCall(content)
			}

			blocks = append(blocks, blockContent{
				blockType: block.BlockType,
				content:   content,
			})
		}
	}

	// Find last content block index
	lastContentIdx := -1
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].blockType == gai.Content {
			lastContentIdx = i
			break
		}
	}

	// Route: last content block to stdout, everything else to stderr
	for i, block := range blocks {
		writer := g.stderr
		if i == lastContentIdx && lastContentIdx != -1 {
			writer = g.stdout
		}
		fmt.Fprint(writer, block.content)
	}

	return response, nil
}

func (g *ResponsePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}
