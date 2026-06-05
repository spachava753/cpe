package codemode

import (
	"bytes"
	_ "embed"
	"fmt"
	"runtime"
	"text/template"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/gai"
)

//go:embed tool_description.txt
var toolDesc string

var toolDescTmpl = template.Must(
	template.New("tool_desc").Parse(toolDesc),
)

// GenerateToolDescription builds the authoritative prompt shown in the
// execute_go_code tool schema. It documents runtime constraints and the required
// Run(ctx) file contract.
func GenerateToolDescription() string {
	var b bytes.Buffer
	err := toolDescTmpl.Execute(&b, struct {
		GoVersion string
	}{
		GoVersion: runtime.Version(),
	})
	if err != nil {
		panic(fmt.Errorf("execute execute_go_code description template: %w", err))
	}
	return b.String()
}

// MakeTool returns the execute_go_code tool definition consumed
// by the agent runtime, including bounded timeout validation in its input schema.
func MakeTool(maxTimeout int) gai.Tool {
	description := GenerateToolDescription()

	if maxTimeout <= 0 {
		maxTimeout = 300
	}

	// Build input schema per spec:
	// - code: string (required) - Complete Go source file contents
	// - executionTimeout: integer (required, min 1, max maxTimeout) - Maximum execution time in seconds
	minTimeout := 1.0
	maxTimeoutF := float64(maxTimeout)

	inputSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"code": {
				Type:        "string",
				Description: "Complete Go source file contents implementing the Run function",
			},
			"executionTimeout": {
				Type:        "integer",
				Description: fmt.Sprintf("Maximum execution time in seconds (1-%d). Estimate based on expected runtime of the generated code.", maxTimeout),
				Minimum:     &minTimeout,
				Maximum:     &maxTimeoutF,
			},
		},
		Required: []string{"code", "executionTimeout"},
	}

	return gai.Tool{
		Name:        ExecuteGoCodeToolName,
		Description: description,
		InputSchema: inputSchema,
	}
}
