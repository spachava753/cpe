package codemode

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"
)

// GenerateExecuteGoCodeDescription generates the markdown description for the
// execute_go_code tool. It includes the Go version, available function signatures
// and types, code structure template, and usage instructions.
func GenerateExecuteGoCodeDescription(tools []*mcp.Tool) (string, error) {
	goVersion := runtime.Version()

	// Generate type definitions and function signatures
	toolDefs, err := GenerateToolDefinitions(tools)
	if err != nil {
		return "", fmt.Errorf("generating tool definitions: %w", err)
	}

	var b strings.Builder

	// Opening paragraph with Go version
	b.WriteString(fmt.Sprintf("Execute generated Golang code. The version of Go is %s. ", goVersion))
	b.WriteString("You must generate a complete Go source file that implements the `Run(ctx context.Context) error` function. ")
	b.WriteString("The file will be compiled alongside a `main.go` that calls your `Run` function.\n\n")

	// Available functions and types section (only if tools exist)
	if toolDefs != "" {
		b.WriteString("Keep in mind you have access to the following functions and types when generating code:\n")
		b.WriteString("```go\n")
		b.WriteString(toolDefs)
		b.WriteString("\n```\n\n")
	}

	// Helper function documentation
	b.WriteString("A `ptr[T any](v T) *T` helper function is available to create pointers from literals for optional fields. ")
	b.WriteString("For example: `ptr(\"hello\")` returns `*string`, `ptr(42)` returns `*int`, `ptr(3.14)` returns `*float64`.\n\n")

	// Code structure template
	b.WriteString("Your generated code should be a complete Go file with the following structure:\n")
	b.WriteString("```go\n")
	b.WriteString(`package main

import (
	"context"
	"fmt"
	// add other imports as needed
)

func Run(ctx context.Context) error {
	// your implementation here
	return nil
}
`)
	b.WriteString("```\n\n")

	// main.go shape explanation
	b.WriteString("The `main.go` file (which you don't need to generate) will have the following shape:\n")
	b.WriteString("```go\n")
	b.WriteString(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	// and other std packages
)

// generated types and function definitions
// ...

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	
	// setup code that initializes the generated functions
	// ...
	
	err := Run(ctx)
	if err != nil {
		fmt.Printf("\nexecution error: %s\n", err)
		os.Exit(1)
	}
}
`)
	b.WriteString("```\n\n")

	// Error handling note
	b.WriteString("The error, if not nil, returned from the `Run` function, will be present in the tool result.\n\n")

	// Important note about complete file generation
	b.WriteString("IMPORTANT: Generate the complete file contents including package declaration and imports. ")
	b.WriteString("This ensures that any compilation errors report accurate line numbers that you can use for debugging.")

	return b.String(), nil
}

// GenerateExecuteGoCodeTool generates the complete gai.Tool definition for the
// execute_go_code tool, including its description and input schema.
func GenerateExecuteGoCodeTool(tools []*mcp.Tool) (gai.Tool, error) {
	description, err := GenerateExecuteGoCodeDescription(tools)
	if err != nil {
		return gai.Tool{}, fmt.Errorf("generating description: %w", err)
	}

	// Build input schema per spec:
	// - code: string (required) - Complete Go source file contents
	// - executionTimeout: integer (required, min 1, max 300) - Maximum execution time in seconds
	minTimeout := 1.0
	maxTimeout := 300.0

	inputSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"code": {
				Type:        "string",
				Description: "Complete Go source file contents implementing the Run function",
			},
			"executionTimeout": {
				Type:        "integer",
				Description: "Maximum execution time in seconds (1-300). Estimate based on expected runtime of the generated code.",
				Minimum:     &minTimeout,
				Maximum:     &maxTimeout,
			},
		},
		Required: []string{"code", "executionTimeout"},
	}

	return gai.Tool{
		Name:        ExecuteGoCodeToolName,
		Description: description,
		InputSchema: inputSchema,
	}, nil
}
