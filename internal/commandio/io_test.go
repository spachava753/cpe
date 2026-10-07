package commandio_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/commandio"
)

func TestIO(t *testing.T) {
	input := strings.NewReader("input")
	var output, diagnostics bytes.Buffer
	for _, test := range []struct {
		name    string
		streams commandio.IO
		in      io.Reader
		out     io.Writer
		err     io.Writer
	}{
		{
			name: "process streams",
			in:   os.Stdin,
			out:  os.Stdout,
			err:  os.Stderr,
		},
		{
			name: "supplied streams",
			streams: commandio.IO{
				In:  input,
				Out: &output,
				Err: &diagnostics,
			},
			in:  input,
			out: &output,
			err: &diagnostics,
		},
		{
			name: "independent output default",
			streams: commandio.IO{
				In:  input,
				Err: &diagnostics,
			},
			in:  input,
			out: os.Stdout,
			err: &diagnostics,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.streams.Input() != test.in || test.streams.Output() != test.out || test.streams.ErrorOutput() != test.err {
					t.Fatal("command streams were replaced or defaulted incorrectly")
				}
			},
		)
	}
}

type descriptorWriter struct {
	io.Writer
	descriptor uintptr
}

func (writer descriptorWriter) Fd() uintptr { return writer.descriptor }

func TestUseJSON(t *testing.T) {
	var output bytes.Buffer
	reader, pipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = pipe.Close() })
	type jsonCase struct {
		name      string
		output    io.Writer
		requested bool
		want      bool
	}
	cases := []jsonCase{
		{
			name:   "buffer",
			output: &output,
			want:   true,
		},
		{
			name: "pipe",
			output: descriptorWriter{
				Writer:     &output,
				descriptor: pipe.Fd(),
			},
			want: true,
		},
		{
			name:      "explicit JSON",
			output:    &output,
			requested: true,
			want:      true,
		},
	}
	terminal, err := os.OpenFile(
		"/dev/ptmx",
		os.O_RDWR,
		0,
	)
	if err == nil {
		t.Cleanup(func() { _ = terminal.Close() })
		writer := descriptorWriter{
			Writer:     &output,
			descriptor: terminal.Fd(),
		}
		cases = append(
			cases,
			jsonCase{
				name:   "terminal",
				output: writer,
			},
			jsonCase{
				name:      "forced terminal JSON",
				output:    writer,
				requested: true,
				want:      true,
			},
		)
	} else if runtime.GOOS == "linux" {
		t.Fatal(err)
	}
	t.Setenv(
		"CLICOLOR_FORCE",
		"1",
	)
	t.Setenv(
		"FORCE_COLOR",
		"1",
	)
	for _, test := range cases {
		t.Run(
			test.name,
			func(t *testing.T) {
				if actual := commandio.UseJSON(
					test.output,
					test.requested,
				); actual != test.want {
					t.Fatalf(
						"JSON output = %v, want %v",
						actual,
						test.want,
					)
				}
			},
		)
	}
}

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestWriteTable(t *testing.T) {
	writeErr := errors.New("write failed")
	for _, test := range []struct {
		name     string
		rows     [][]string
		writeErr error
		wantErr  error
	}{
		{
			name:     "empty results do not write",
			writeErr: writeErr,
		},
		{
			name: "formatted results",
			rows: [][]string{
				{
					"alpha",
					"location",
				},
			},
		},
		{
			name: "writer failure",
			rows: [][]string{
				{
					"alpha",
					"location",
				},
			},
			writeErr: writeErr,
			wantErr:  writeErr,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var output bytes.Buffer
				var writer io.Writer = &output
				if test.writeErr != nil {
					writer = failingWriter{
						err: test.writeErr,
					}
				}
				err := commandio.WriteTable(
					writer,
					[]string{
						"Name",
						"Location",
					},
					test.rows,
				)
				if !errors.Is(
					err,
					test.wantErr,
				) {
					t.Fatalf(
						"write error = %v, want %v",
						err,
						test.wantErr,
					)
				}
				if test.writeErr == nil && len(test.rows) > 0 {
					for _, expected := range []string{
						"Name",
						"Location",
						"alpha",
						"location",
					} {
						if !strings.Contains(
							output.String(),
							expected,
						) {
							t.Fatalf(
								"output %q does not contain %q",
								output.String(),
								expected,
							)
						}
					}
				}
			},
		)
	}
}
