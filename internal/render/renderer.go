package render

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	goldmarktext "github.com/yuin/goldmark/text"
	"golang.org/x/term"
)

const (
	defaultWordWrapWidth  = 120
	disabledWordWrapWidth = 0
	wordWrapGutter        = 4
)

// Renderer is an interface for rendering content (e.g., markdown to formatted output).
type Iface interface {
	Render(in string) (string, error)
}

// PlainTextRenderer renders markdown with Glamour's ASCII style and no width-based wrapping.
type PlainTextRenderer struct {
	renderer *GlamourRenderer
}

// NewPlainTextRenderer creates a renderer for non-TTY/plain terminal output.
func NewPlainTextRenderer() *PlainTextRenderer {
	return &PlainTextRenderer{renderer: newTermRenderer(
		glamour.WithStyles(plainTextStyle()),
		glamour.WithColorProfile(termenv.Ascii),
		glamour.WithWordWrap(disabledWordWrapWidth),
	)}
}

// Render renders markdown for plain terminal display without ANSI styling or wrapping.
func (p *PlainTextRenderer) Render(in string) (string, error) {
	return p.renderer.Render(in)
}

// GlamourRenderer renders markdown through glamour after normalizing terminal-sensitive code blocks.
type GlamourRenderer struct {
	renderer *glamour.TermRenderer
}

// Render renders markdown for terminal display.
func (r *GlamourRenderer) Render(in string) (string, error) {
	return r.renderer.Render(expandCodeBlockTabs(in))
}

// IsTTYWriter returns true when w is backed by a terminal file descriptor.
func IsTTYWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func baseStyle() ansi.StyleConfig {
	style := styles.LightStyleConfig
	if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}
	return withoutLeadingDocumentBlock(style)
}

func plainTextStyle() ansi.StyleConfig {
	return withoutLeadingDocumentBlock(styles.ASCIIStyleConfig)
}

func withoutLeadingDocumentBlock(style ansi.StyleConfig) ansi.StyleConfig {
	style.Document.BlockPrefix = ""
	return style
}

func writerWordWrapWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return defaultWordWrapWidth
	}

	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil || width <= 0 {
		return defaultWordWrapWidth
	}
	if width <= wordWrapGutter {
		return width
	}
	return width - wordWrapGutter
}

func expandCodeBlockTabs(markdown string) string {
	if !strings.Contains(markdown, "\t") {
		return markdown
	}

	source := []byte(markdown)
	segments := codeBlockLineSegments(source)
	if len(segments) == 0 {
		return markdown
	}

	var b strings.Builder
	b.Grow(len(markdown))
	last := 0
	for _, segment := range segments {
		b.Write(source[last:segment.Start])
		b.WriteString(expandTabsToSpaces(string(source[segment.Start:segment.Stop])))
		last = segment.Stop
	}
	b.Write(source[last:])
	return b.String()
}

func codeBlockLineSegments(source []byte) []goldmarktext.Segment {
	doc := goldmark.New().Parser().Parse(goldmarktext.NewReader(source))
	var segments []goldmarktext.Segment
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := node.Lines()
			for i := 0; i < lines.Len(); i++ {
				segments = append(segments, lines.At(i))
			}
		}
		return ast.WalkContinue, nil
	})
	return segments
}

func expandTabsToSpaces(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	column := 0
	for _, r := range s {
		if r == '\t' {
			spaces := 8 - column%8
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			continue
		}
		b.WriteRune(r)
		column++
	}
	return b.String()
}

func newTermRenderer(options ...glamour.TermRendererOption) *GlamourRenderer {
	renderer, err := glamour.NewTermRenderer(options...)
	if err != nil {
		panic(err.Error())
	}
	return &GlamourRenderer{renderer: renderer}
}

// NewGlamourRendererForWriter creates a glamour renderer sized to the terminal backing w.
func NewGlamourRendererForWriter(w io.Writer) *GlamourRenderer {
	return newTermRenderer(
		glamour.WithStyles(baseStyle()),
		glamour.WithWordWrap(writerWordWrapWidth(w)),
	)
}

// NewThinkingRendererForWriter creates a thinking renderer sized to the terminal backing w.
func NewThinkingRendererForWriter(w io.Writer) *GlamourRenderer {
	return newTermRenderer(
		glamour.WithStyles(thinkingStyle()),
		glamour.WithWordWrap(writerWordWrapWidth(w)),
	)
}

func thinkingStyle() ansi.StyleConfig {
	style := baseStyle()
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
	return style
}
