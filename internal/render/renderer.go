package render

import (
	"io"
	"os"
	"strconv"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Renderer is an interface for rendering content (e.g., markdown to formatted output).
type Iface interface {
	Render(in string) (string, error)
}

// PlainTextRenderer is a renderer that returns content as-is without formatting.
// Used as a fallback when glamour rendering fails.
type PlainTextRenderer struct{}

// Render returns the input unchanged.
func (p *PlainTextRenderer) Render(in string) (string, error) {
	return in, nil
}

// IsTTYWriter returns true when w is backed by a terminal file descriptor.
func IsTTYWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func getBaseStyle() ansi.StyleConfig {
	style := styles.LightStyleConfig
	if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}
	style.Document.BlockPrefix = ""
	return style
}

// NewGlamourRenderer creates a glamour renderer with appropriate styling for TTY contexts.
func NewGlamourRenderer() *glamour.TermRenderer {
	style := getBaseStyle()

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(120),
	)

	if err != nil {
		panic(err.Error())
	}

	return r
}

func NewThinkingRenderer() *glamour.TermRenderer {
	style := getBaseStyle()
	textColor, err := strconv.Atoi(*style.Document.Color)
	if err != nil {
		panic(err.Error())
	}
	if termenv.HasDarkBackground() {
		textColor -= 4
	} else {
		textColor += 4
	}
	thinkingTextColor := strconv.Itoa(textColor)
	style.Text.Color = &thinkingTextColor
	style.Document.BlockSuffix = "\n"

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		panic(err.Error())
	}
	return renderer
}
