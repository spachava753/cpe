package agent

import (
	"os"
	"strconv"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Renderer is an interface for rendering markdown content
type Renderer interface {
	Render(in string) (string, error)
}

// PlainTextRenderer is a renderer that returns content as-is without formatting.
// Used for non-TTY contexts where ANSI codes should not be present.
type PlainTextRenderer struct{}

// Render returns the input unchanged
func (p *PlainTextRenderer) Render(in string) (string, error) {
	return in, nil
}

// IsTTY returns true if stdout is connected to a terminal
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// getBaseStyle returns the appropriate glamour style based on terminal background.
func getBaseStyle() ansi.StyleConfig {
	style := styles.LightStyleConfig
	if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}
	style.Document.BlockPrefix = ""
	return style
}

// NewGlamourRenderer creates a glamour renderer with appropriate styling for TTY contexts.
func NewGlamourRenderer() (*glamour.TermRenderer, error) {
	style := getBaseStyle()

	return glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(120),
	)
}

// NewRenderer creates a renderer appropriate for the current context.
// Returns a glamour renderer for TTY contexts, or a plain text renderer otherwise.
func NewRenderer() Renderer {
	if !IsTTY() {
		return &PlainTextRenderer{}
	}

	renderer, err := NewGlamourRenderer()
	if err != nil {
		return &PlainTextRenderer{}
	}
	return renderer
}

// ResponsePrinterRenderers holds the three renderers used by ResponsePrinterGenerator.
type ResponsePrinterRenderers struct {
	Content  Renderer
	Thinking Renderer
	ToolCall Renderer
}

// NewResponsePrinterRenderers creates the appropriate renderers for ResponsePrinterGenerator.
// For TTY contexts, creates styled glamour renderers (with distinct thinking style).
// For non-TTY contexts, returns plain text renderers.
func NewResponsePrinterRenderers() ResponsePrinterRenderers {
	if !IsTTY() {
		plain := &PlainTextRenderer{}
		return ResponsePrinterRenderers{
			Content:  plain,
			Thinking: plain,
			ToolCall: plain,
		}
	}

	style := getBaseStyle()

	contentRenderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		plain := &PlainTextRenderer{}
		return ResponsePrinterRenderers{Content: plain, Thinking: plain, ToolCall: plain}
	}

	// Thinking style has different text color
	thinkingStyle := style
	textColor, err := strconv.Atoi(*thinkingStyle.Document.Color)
	if err != nil {
		// Use content renderer for thinking if we can't parse the color
		return ResponsePrinterRenderers{Content: contentRenderer, Thinking: contentRenderer, ToolCall: contentRenderer}
	}
	if termenv.HasDarkBackground() {
		textColor = textColor - 4
	} else {
		textColor = textColor + 4
	}
	thinkingTextColor := strconv.Itoa(textColor)
	thinkingStyle.Text.Color = &thinkingTextColor
	thinkingStyle.Document.BlockSuffix = "\n"

	thinkingRenderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(thinkingStyle),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		plain := &PlainTextRenderer{}
		return ResponsePrinterRenderers{Content: plain, Thinking: plain, ToolCall: plain}
	}

	return ResponsePrinterRenderers{
		Content:  contentRenderer,
		Thinking: thinkingRenderer,
		ToolCall: contentRenderer,
	}
}
