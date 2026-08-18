package acp

import (
	_ "embed"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
)

//go:embed tool_description.prompt
var toolDesc string

// GenerateToolDescription returns the authoritative starlark_repl prompt.
func GenerateToolDescription() string {
	return toolDesc
}

// MakeTool returns the starlark_repl definition consumed by the agent runtime.
func MakeTool(maxTimeout int) gai.Tool {
	if maxTimeout <= 0 {
		maxTimeout = 300
	}

	minTimeout := 1.0
	maxTimeoutF := float64(maxTimeout)
	inputSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"code": {
				Type:        "string",
				Description: "Starlark statements and expressions to evaluate in the session REPL",
			},
			"executionTimeout": {
				Type:        "integer",
				Description: fmt.Sprintf("Maximum execution time in seconds (1-%d). Estimate based on expected runtime.", maxTimeout),
				Minimum:     &minTimeout,
				Maximum:     &maxTimeoutF,
			},
		},
		Required: []string{"code", "executionTimeout"},
	}

	return gai.Tool{
		Name:        StarlarkREPLToolName,
		Description: GenerateToolDescription(),
		InputSchema: inputSchema,
	}
}
