package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"github.com/spachava753/gai"
	"golang.org/x/term"
)

type Renderer interface {
	Render(in string) (string, error)
}

// ResponsePrinterGenerator is a wrapper around another generator that prints out
// the response returned from the wrapped generator with styled markdown rendering.
type ResponsePrinterGenerator struct {
	// wrapped is the generator being wrapped
	wrapped gai.ToolCapableGenerator

	// contentRenderer renders content blocks
	contentRenderer Renderer

	// thinkingRenderer renders thinking blocks
	thinkingRenderer Renderer

	// toolCallRenderer renders tool call blocks
	toolCallRenderer Renderer
}

// NewResponsePrinterGenerator creates a new ResponsePrinterGenerator with initialized glamour renderers
func NewResponsePrinterGenerator(wrapped gai.ToolCapableGenerator) *ResponsePrinterGenerator {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		style := styles.NoTTYStyleConfig

		style.Document.BlockPrefix = ""

		contentRenderer, _ := glamour.NewTermRenderer(
			glamour.WithStyles(styles.NoTTYStyleConfig),
		)

		thinkingRenderer, _ := glamour.NewTermRenderer(
			glamour.WithStyles(styles.NoTTYStyleConfig),
		)

		return &ResponsePrinterGenerator{
			wrapped:          wrapped,
			contentRenderer:  contentRenderer,
			thinkingRenderer: thinkingRenderer,
			toolCallRenderer: contentRenderer,
		}
	}

	style := styles.LightStyleConfig
	if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}

	style.Document.BlockPrefix = ""

	// we want the text of the thinking style to be a bit muted to differentiate from normal content text
	thinkingStyle := style
	textColor, _ := strconv.Atoi(*thinkingStyle.Document.Color)
	textColor = textColor - 4
	thinkingTextColor := strconv.Itoa(textColor)
	thinkingStyle.Text.Color = &thinkingTextColor
	thinkingStyle.Document.BlockSuffix = "\n"

	toolcallStyle := style
	toolcallStyle.Document.BlockSuffix = ""

	contentRenderer, _ := glamour.NewTermRenderer(
		glamour.WithStyles(style),
	)

	thinkingRenderer, _ := glamour.NewTermRenderer(
		glamour.WithStyles(thinkingStyle),
	)

	return &ResponsePrinterGenerator{
		wrapped:          wrapped,
		contentRenderer:  contentRenderer,
		thinkingRenderer: thinkingRenderer,
		toolCallRenderer: contentRenderer,
	}
}

// renderContent renders a content block with glamour
func (g *ResponsePrinterGenerator) renderContent(content string) string {
	rendered, err := g.contentRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		// Fallback to plain text if rendering fails
		return content
	}

	return rendered
}

// renderThinking renders a thinking block with a muted glamour style
func (g *ResponsePrinterGenerator) renderThinking(content string) string {
	rendered, err := g.thinkingRenderer.Render(strings.TrimSpace(content))
	if err != nil {
		// Fallback to plain text if rendering fails
		return content
	}

	return rendered
}

// unescapeJsonString unescapes special characters in json content that can returned by some models in tool calls
func unescapeJsonString(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+5 < len(s) && s[i:i+2] == "\\u" {
			hex := s[i+2 : i+6]
			var code int
			_, err := fmt.Sscanf(hex, "%04x", &code)
			if err == nil {
				r := rune(code)
				switch r {
				case '"':
					result.WriteString(`\"`)
				case '\\':
					result.WriteString(`\\`)
				case '\b':
					result.WriteString(`\b`)
				case '\f':
					result.WriteString(`\f`)
				case '\n':
					result.WriteString(`\n`)
				case '\r':
					result.WriteString(`\r`)
				case '\t':
					result.WriteString(`\t`)
				default:
					result.WriteRune(r)
				}
				i += 6
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// renderToolCall renders a toolcall block with a glamour style
func (g *ResponsePrinterGenerator) renderToolCall(content string) string {
	var formattedJson bytes.Buffer
	if err := json.Indent(&formattedJson, []byte(unescapeJsonString(content)), "", "  "); err != nil {
		panic(err)
	}
	result := fmt.Sprintf("#### [tool call]\n```json\n%s\n```", formattedJson.String())

	rendered, err := g.toolCallRenderer.Render(result)
	if err != nil {
		// Fallback to plain text if rendering fails
		return content
	}

	return rendered
}

// Generate implements the gai.Generator interface.
// It calls the wrapped generator's Generate method and then prints the response with styled markdown rendering.
func (g *ResponsePrinterGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	// Call the wrapped generator
	response, err := g.wrapped.Generate(ctx, dialog, options)
	if err != nil {
		return gai.Response{}, err
	}

	var sb strings.Builder
	var hasToolcalls bool
	// Print the response with markdown rendering
	for _, candidate := range response.Candidates {
		for _, block := range candidate.Blocks {
			if block.ModalityType != gai.Text {
				fmt.Fprintf(&sb, "Received non-text block of type: %s\n", block.ModalityType)
			}

			hasToolcalls = block.BlockType == gai.ToolCall || hasToolcalls

			// Render content based on block type
			content := block.Content.String()
			switch block.BlockType {
			case gai.Content:
				content = g.renderContent(content)
			case gai.Thinking:
				content = g.renderThinking(content)
			case gai.ToolCall:
				content = g.renderToolCall(content)
			default:
				// For unknown block types, render as-is
			}

			fmt.Fprint(&sb, content)
		}
	}

	if hasToolcalls {
		fmt.Fprint(os.Stderr, sb.String())
	} else {
		fmt.Fprint(os.Stdout, sb.String())
	}

	return response, nil
}

// Register implements the gai.ToolRegister interface by delegating to the wrapped generator.
func (g *ResponsePrinterGenerator) Register(tool gai.Tool) error {
	return g.wrapped.Register(tool)
}
