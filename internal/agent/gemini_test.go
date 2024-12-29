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
		{
			name:  "example 1",
			input: `import (\n\t\"fmt\"\n\t\"github.com/anthropics/anthropic-sdk-go\"\n\t\"github.com/openai/openai-go\"\n\t\"log/slog\"\n)`,
			expected: `import (
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"log/slog"
)`,
		},
		{
			name:  "example 2",
			input: `if flags.CustomURL == \"\" {\n\t\t\treturn GenConfig{}, fmt.Errorf(\"unknown model \'%s\' requires -custom-url flag or CPE_CUSTOM_URL environment variable\", modelName)\n\t\t}\n\t\tlogger.Info(\"Using unknown model with OpenAI provider\", slog.String(\"model\", modelName), slog.String(\"custom-url\", flags.CustomURL))`,
			expected: `if flags.CustomURL == "" {
			return GenConfig{}, fmt.Errorf("unknown model '%s' requires -custom-url flag or CPE_CUSTOM_URL environment variable", modelName)
		}
		logger.Info("Using unknown model with OpenAI provider", slog.String("model", modelName), slog.String("custom-url", flags.CustomURL))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unescapeString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
