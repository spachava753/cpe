package codemode

import (
	"runtime"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGenerateExecuteGoCodeDescription(t *testing.T) {
	tests := []struct {
		name    string
		tools   []*mcp.Tool
		want    string
		wantErr bool
	}{
		{
			name:  "empty tools list",
			tools: []*mcp.Tool{},
			want: `Execute generated Golang code. The version of Go is ` + runtime.Version() + `. You must generate a complete Go source file that implements the ` + "`Run(ctx context.Context) error`" + ` function. The file will be compiled alongside a ` + "`main.go`" + ` that calls your ` + "`Run`" + ` function.

A ` + "`ptr[T any](v T) *T`" + ` helper function is available to create pointers from literals for optional fields. For example: ` + "`ptr(\"hello\")`" + ` returns ` + "`*string`" + `, ` + "`ptr(42)`" + ` returns ` + "`*int`" + `, ` + "`ptr(3.14)`" + ` returns ` + "`*float64`" + `.

Your generated code should be a complete Go file with the following structure:
` + "```go" + `
package main

import (
	"context"
	"fmt"
	// add other imports as needed
)

func Run(ctx context.Context) error {
	// your implementation here
	return nil
}
` + "```" + `

The ` + "`main.go`" + ` file (which you don't need to generate) will have the following shape:
` + "```go" + `
package main

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
` + "```" + `

The error, if not nil, returned from the ` + "`Run`" + ` function, will be present in the tool result.

IMPORTANT: Generate the complete file contents including package declaration and imports. This ensures that any compilation errors report accurate line numbers that you can use for debugging.`,
		},
		{
			name: "single tool with input and output",
			tools: []*mcp.Tool{
				{
					Name:        "get_weather",
					Description: "Get current weather data for a location",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{
								"type":        "string",
								"description": "The name of the city",
							},
						},
						"required": []any{"city"},
					},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"temperature": map[string]any{
								"type": "number",
							},
						},
					},
				},
			},
			want: `Execute generated Golang code. The version of Go is ` + runtime.Version() + `. You must generate a complete Go source file that implements the ` + "`Run(ctx context.Context) error`" + ` function. The file will be compiled alongside a ` + "`main.go`" + ` that calls your ` + "`Run`" + ` function.

Keep in mind you have access to the following functions and types when generating code:
` + "```go" + `
type GetWeatherInput struct {
	// City The name of the city
	City string ` + "`json:\"city\"`" + `
}

type GetWeatherOutput struct {
	Temperature *float64 ` + "`json:\"temperature,omitempty\"`" + `
}

// GetWeather Get current weather data for a location
var GetWeather func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error)
` + "```" + `

A ` + "`ptr[T any](v T) *T`" + ` helper function is available to create pointers from literals for optional fields. For example: ` + "`ptr(\"hello\")`" + ` returns ` + "`*string`" + `, ` + "`ptr(42)`" + ` returns ` + "`*int`" + `, ` + "`ptr(3.14)`" + ` returns ` + "`*float64`" + `.

Your generated code should be a complete Go file with the following structure:
` + "```go" + `
package main

import (
	"context"
	"fmt"
	// add other imports as needed
)

func Run(ctx context.Context) error {
	// your implementation here
	return nil
}
` + "```" + `

The ` + "`main.go`" + ` file (which you don't need to generate) will have the following shape:
` + "```go" + `
package main

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
` + "```" + `

The error, if not nil, returned from the ` + "`Run`" + ` function, will be present in the tool result.

IMPORTANT: Generate the complete file contents including package declaration and imports. This ensures that any compilation errors report accurate line numbers that you can use for debugging.`,
		},
		{
			name: "tool without output schema uses string",
			tools: []*mcp.Tool{
				{
					Name:        "send_message",
					Description: "Send a message",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{"type": "string"},
						},
					},
					OutputSchema: nil,
				},
			},
			want: `Execute generated Golang code. The version of Go is ` + runtime.Version() + `. You must generate a complete Go source file that implements the ` + "`Run(ctx context.Context) error`" + ` function. The file will be compiled alongside a ` + "`main.go`" + ` that calls your ` + "`Run`" + ` function.

Keep in mind you have access to the following functions and types when generating code:
` + "```go" + `
type SendMessageInput struct {
	Text *string ` + "`json:\"text,omitempty\"`" + `
}

type SendMessageOutput = string

// SendMessage Send a message
var SendMessage func(ctx context.Context, input SendMessageInput) (SendMessageOutput, error)
` + "```" + `

A ` + "`ptr[T any](v T) *T`" + ` helper function is available to create pointers from literals for optional fields. For example: ` + "`ptr(\"hello\")`" + ` returns ` + "`*string`" + `, ` + "`ptr(42)`" + ` returns ` + "`*int`" + `, ` + "`ptr(3.14)`" + ` returns ` + "`*float64`" + `.

Your generated code should be a complete Go file with the following structure:
` + "```go" + `
package main

import (
	"context"
	"fmt"
	// add other imports as needed
)

func Run(ctx context.Context) error {
	// your implementation here
	return nil
}
` + "```" + `

The ` + "`main.go`" + ` file (which you don't need to generate) will have the following shape:
` + "```go" + `
package main

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
` + "```" + `

The error, if not nil, returned from the ` + "`Run`" + ` function, will be present in the tool result.

IMPORTANT: Generate the complete file contents including package declaration and imports. This ensures that any compilation errors report accurate line numbers that you can use for debugging.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateExecuteGoCodeDescription(tt.tools)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateExecuteGoCodeDescription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateExecuteGoCodeDescription() mismatch\ngot:\n%s\n\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestGenerateExecuteGoCodeTool(t *testing.T) {
	tests := []struct {
		name    string
		tools   []*mcp.Tool
		wantErr bool
	}{
		{
			name:  "empty tools",
			tools: []*mcp.Tool{},
		},
		{
			name: "with tools",
			tools: []*mcp.Tool{
				{
					Name:        "test_tool",
					Description: "A test tool",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"param": map[string]any{"type": "string"},
						},
					},
					OutputSchema: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := GenerateExecuteGoCodeTool(tt.tools)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateExecuteGoCodeTool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify tool name
			if tool.Name != ExecuteGoCodeToolName {
				t.Errorf("tool.Name = %q, want %q", tool.Name, ExecuteGoCodeToolName)
			}

			// Verify description is non-empty
			if tool.Description == "" {
				t.Error("tool.Description is empty")
			}

			// Verify input schema structure
			if tool.InputSchema == nil {
				t.Fatal("tool.InputSchema is nil")
			}

			if tool.InputSchema.Type != "object" {
				t.Errorf("InputSchema.Type = %q, want \"object\"", tool.InputSchema.Type)
			}

			// Check required fields
			if len(tool.InputSchema.Required) != 2 {
				t.Errorf("InputSchema.Required length = %d, want 2", len(tool.InputSchema.Required))
			}

			// Check code property
			codeProp, ok := tool.InputSchema.Properties["code"]
			if !ok {
				t.Error("InputSchema.Properties missing 'code'")
			} else {
				if codeProp.Type != "string" {
					t.Errorf("code property type = %q, want \"string\"", codeProp.Type)
				}
			}

			// Check executionTimeout property
			timeoutProp, ok := tool.InputSchema.Properties["executionTimeout"]
			if !ok {
				t.Error("InputSchema.Properties missing 'executionTimeout'")
			} else {
				if timeoutProp.Type != "integer" {
					t.Errorf("executionTimeout property type = %q, want \"integer\"", timeoutProp.Type)
				}
				if timeoutProp.Minimum == nil || *timeoutProp.Minimum != 1 {
					t.Error("executionTimeout.Minimum should be 1")
				}
				if timeoutProp.Maximum == nil || *timeoutProp.Maximum != 300 {
					t.Error("executionTimeout.Maximum should be 300")
				}
			}
		})
	}
}

func TestExecuteGoCodeToolNameConstant(t *testing.T) {
	if ExecuteGoCodeToolName != "execute_go_code" {
		t.Errorf("ExecuteGoCodeToolName = %q, want \"execute_go_code\"", ExecuteGoCodeToolName)
	}
}
