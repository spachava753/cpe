// Package commandio provides command streams, output formatting, and scope resolution.
package commandio

import (
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// IO supplies command input, result output, and diagnostics.
// Its accessors use the process standard streams for nil fields.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Input returns In, or os.Stdin when In is nil.
func (streams IO) Input() io.Reader {
	if streams.In != nil {
		return streams.In
	}
	return os.Stdin
}

// Output returns Out, or os.Stdout when Out is nil.
func (streams IO) Output() io.Writer {
	if streams.Out != nil {
		return streams.Out
	}
	return os.Stdout
}

// ErrorOutput returns Err, or os.Stderr when Err is nil.
func (streams IO) ErrorOutput() io.Writer {
	if streams.Err != nil {
		return streams.Err
	}
	return os.Stderr
}

// UseJSON reports whether command results should be rendered as JSON.
// It returns true if requested is true or output is not a terminal.
// Writers must expose Fd() uintptr for terminal detection; otherwise JSON is used.
func UseJSON(output io.Writer, requested bool) bool {
	if requested {
		return true
	}
	terminal, ok := output.(interface{ Fd() uintptr })
	return !ok || !term.IsTerminal(terminal.Fd())
}
