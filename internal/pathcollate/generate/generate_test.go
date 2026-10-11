package generate

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestTables(t *testing.T) {
	failure := errors.New("reader failed")
	for _, test := range []struct {
		name  string
		input io.Reader
		want  string
	}{
		{
			name:  "source identity must match the pin",
			input: strings.NewReader("changed collation data"),
			want:  "does not match pinned ICU 78.3",
		},
		{
			name: "source read errors are retained",
			input: failingReader{
				err: failure,
			},
			want: failure.Error(),
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var output bytes.Buffer
				err := Tables(
					test.input,
					&output,
				)
				if err == nil || !strings.Contains(
					err.Error(),
					test.want,
				) || output.Len() != 0 {
					t.Fatalf(
						"Tables = %v, %d output bytes; want %q with no output",
						err,
						output.Len(),
						test.want,
					)
				}
			},
		)
	}
}

func TestParse(t *testing.T) {
	for _, test := range []struct {
		name        string
		input       string
		wantEntries int
		wantHan     []rune
		wantError   string
	}{
		{
			name: "weights contractions prefixes and Han references",
			input: "[radical 1=⼀:一丁-七]\n" +
				"0061; [2B, 05, 05]\n" +
				"00E1; [2B, 05, 05][, 88, 05]\n" +
				"004C | 00B7; [, FB B6, 05]\n" +
				"0E40 0E01; [74 06, 05, 05][74 7A, 05, 05]\n" +
				"3220; [U+4E00, 10]\n",
			wantEntries: 4,
			wantHan: []rune{
				'一',
				'丁',
				'丂',
				'七',
			},
		},
		{
			name:      "invalid radical line",
			input:     "[radical 1]\n",
			wantError: "invalid radical line",
		},
		{
			name:      "missing separator",
			input:     "0061 [2B, 05, 05]\n",
			wantError: "invalid collation record",
		},
		{
			name:      "invalid code point",
			input:     "110001; [2B, 05, 05]\n",
			wantError: "invalid collation string",
		},
		{
			name:      "unknown Han reference",
			input:     "3220; [U+4E00]\n",
			wantError: "unknown Han reference",
		},
		{
			name:      "unterminated element",
			input:     "0061; [2B, 05, 05\n",
			wantError: "unterminated collation weights",
		},
		{
			name:      "oversized weight",
			input:     "0061; [2B 00 00 00 00, 05, 05]\n",
			wantError: "exceeds 4 bytes",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				entries, han, err := parse([]byte(test.input))
				if test.wantError != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.wantError,
					) {
						t.Fatalf(
							"parse = %v; want %q",
							err,
							test.wantError,
						)
					}
					return
				}
				if err != nil || len(entries) != test.wantEntries || !slices.Equal(
					han,
					test.wantHan,
				) {
					t.Fatalf(
						"parse = %d entries, %U, %v; want %d entries, %U",
						len(entries),
						han,
						err,
						test.wantEntries,
						test.wantHan,
					)
				}
				if entries[0].weights[0] != 0x2b00000005000500 || entries[1].weights[0] != 0x00000000fbb60500 || entries[3].weights[0] != 0xfd02020005001000 {
					t.Fatalf(
						"parsed collation weights differ from fractional/alias weights: %v",
						entries,
					)
				}
			},
		)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
