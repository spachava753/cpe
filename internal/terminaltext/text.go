// Package terminaltext prepares untrusted text for human-readable terminal output.
package terminaltext

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// The ANSI parser recognizes byte-oriented C1 sequences. Normalize their UTF-8
// spellings to equivalent ESC sequences so their payloads are stripped as well.
var c1Sequences = strings.NewReplacer(
	"\u0090",
	"\x1bP",
	"\u0098",
	"\x1bX",
	"\u009b",
	"\x1b[",
	"\u009c",
	"\x1b\\",
	"\u009d",
	"\x1b]",
	"\u009e",
	"\x1b^",
	"\u009f",
	"\x1b_",
)

// Sanitize strips terminal instructions and controls before trusted styling.
// Ordinary quotes, backslashes, spaces, line feeds, and tabs remain unchanged.
func Sanitize(value string) string {
	return strings.Map(
		func(character rune) rune {
			if character != '\n' && character != '\t' && unicode.IsControl(character) {
				return -1
			}
			return character
		},
		ansi.Strip(c1Sequences.Replace(value)),
	)
}
