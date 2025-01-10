package agent

import "strings"

// unescapeString unescapes a string that contains escaped characters like quotes and whitespace
func unescapeString(s string) string {
	// Replace escaped backslashes with a temporary marker
	s = strings.ReplaceAll(s, `\\`, "\u0000")

	// Replace escaped quotes with actual quotes
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\'`, `'`)

	// Replace escaped newlines and tabs
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")

	// Restore backslashes
	s = strings.ReplaceAll(s, "\u0000", `\`)

	return s
}
