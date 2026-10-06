package plugins_test

import (
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/charmbracelet/x/term"
)

// Capture output without writing to the terminal device. The real descriptor
// exercises terminal detection and writer-aware formatting at the public seam.
type commandFDWriter struct {
	io.Writer
	descriptor uintptr
}

func (writer commandFDWriter) Fd() uintptr { return writer.descriptor }

func commandTerminal(t *testing.T, output io.Writer) io.Writer {
	t.Helper()
	terminal, err := os.OpenFile(
		"/dev/ptmx",
		os.O_RDWR,
		0,
	)
	if err != nil {
		if runtime.GOOS == "linux" {
			t.Fatal(err)
		}
		t.Skip("pseudo-terminal unavailable")
	}
	t.Cleanup(func() { _ = terminal.Close() })
	if !term.IsTerminal(terminal.Fd()) {
		t.Fatal("pseudo-terminal descriptor is not a terminal")
	}
	return commandFDWriter{
		Writer:     output,
		descriptor: terminal.Fd(),
	}
}

func commandPipe(t *testing.T, output io.Writer) io.Writer {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	return commandFDWriter{
		Writer:     output,
		descriptor: writer.Fd(),
	}
}
