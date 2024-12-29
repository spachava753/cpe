package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnescapeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "unescaped string",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "escaped quotes",
			input:    "\\\"hello\\\"",
			expected: "\"hello\"",
		},
		{
			name:     "escaped newline",
			input:    "\\n\\t",
			expected: "\n\t",
		},
		{
			name:     "escaped quotes in string",
			input:    "if flags.CustomURL == \\\"\\\" && os.Getenv(\\\"CPE_CUSTOM_URL\\\") == \\\"\\\"",
			expected: "if flags.CustomURL == \"\" && os.Getenv(\"CPE_CUSTOM_URL\") == \"\"",
		},
		{
			name:     "escaped newline with text",
			input:    "\\n\\tcustomURL := flags.CustomURL\\n",
			expected: "\n\tcustomURL := flags.CustomURL\n",
		},
		{
			name:     "invalid escape sequence",
			input:    "\\k",
			expected: "\\k",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unescapeString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
