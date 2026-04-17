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

	"github.com/spachava753/cpe/internal/ports"
)

// PlainTextRenderer is a renderer that returns content as-is without formatting.
// Used as a fallback when glamour rendering fails.
type PlainTextRenderer struct{}

// Render returns the input unchanged.
func (p *PlainTextRenderer) Render(in string) (string, error) {
	return in, nil
}

// IsTTY returns true if stdout is connected to a terminal.
func IsTTY() bool {
	return IsTTYWriter(os.Stdout)
}

// IsTTYWriter returns true when w is backed by a terminal file descriptor.
func IsTTYWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// TurnLifecycleRenderers holds the three renderers used by
// agent.TurnLifecycleMiddleware.
type TurnLifecycleRenderers struct {
	Content  ports.Renderer
	Thinking ports.Renderer
	ToolCall ports.Renderer
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
func NewGlamourRenderer() (*glamour.TermRenderer, error) {
	style := getBaseStyle()

	return glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(120),
	)
}

func newRendererForTTY(isTTY bool) ports.Renderer {
	if !isTTY {
		return &PlainTextRenderer{}
	}

	renderer, err := NewGlamourRenderer()
	if err != nil {
		return &PlainTextRenderer{}
	}
	return renderer
}

func newThinkingRendererForTTY(isTTY bool) ports.Renderer {
	if !isTTY {
		return &PlainTextRenderer{}
	}

	style := getBaseStyle()
	textColor, err := strconv.Atoi(*style.Document.Color)
	if err != nil {
		return newRendererForTTY(isTTY)
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
		return &PlainTextRenderer{}
	}
	return renderer
}

// NewRenderer creates a renderer appropriate for the current context.
// Returns a glamour renderer for TTY contexts, or plain text passthrough otherwise.
func NewRenderer() ports.Renderer {
	return NewRendererForWriter(os.Stdout)
}

// NewRendererForWriter creates a renderer appropriate for the target writer.
func NewRendererForWriter(w io.Writer) ports.Renderer {
	return newRendererForTTY(IsTTYWriter(w))
}

func newTurnLifecycleRenderers(contentTTY, auxiliaryTTY bool) TurnLifecycleRenderers {
	contentRenderer := newRendererForTTY(contentTTY)
	thinkingRenderer := newThinkingRendererForTTY(auxiliaryTTY)
	toolCallRenderer := newRendererForTTY(auxiliaryTTY)
	return TurnLifecycleRenderers{
		Content:  contentRenderer,
		Thinking: thinkingRenderer,
		ToolCall: toolCallRenderer,
	}
}

func newTurnLifecycleRenderersForTTY(isTTY bool) TurnLifecycleRenderers {
	return newTurnLifecycleRenderers(isTTY, isTTY)
}

// NewTurnLifecycleRenderers creates the appropriate renderers for turn-lifecycle output.
// For TTY contexts, creates styled glamour renderers with a distinct thinking style.
// For non-TTY contexts, returns plain text passthrough renderers.
func NewTurnLifecycleRenderers() TurnLifecycleRenderers {
	return NewTurnLifecycleRenderersForWriters(os.Stdout, os.Stderr)
}

// NewTurnLifecycleRenderersForWriters creates turn-lifecycle renderers tuned
// to the streams they will be written to.
func NewTurnLifecycleRenderersForWriters(stdout io.Writer, stderr io.Writer) TurnLifecycleRenderers {
	return newTurnLifecycleRenderers(IsTTYWriter(stdout), IsTTYWriter(stderr))
}
