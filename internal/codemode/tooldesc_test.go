package codemode

import (
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	schemaTypeObject  = "object"
	schemaTypeString  = "string"
	schemaTypeInteger = "integer"
)

func TestGenerateExecuteGoCodeDescription(t *testing.T) {
	tests := []struct {
		name    string
		tools   []*mcp.Tool
		wantErr bool
	}{
		{
			name:  "empty tools list",
			tools: []*mcp.Tool{},
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateExecuteGoCodeDescription(tt.tools)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateExecuteGoCodeDescription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestGenerateExecuteGoCodeDescription_MultimediaDocumentation(t *testing.T) {
	got, err := GenerateExecuteGoCodeDescription([]*mcp.Tool{})
	if err != nil {
		t.Fatalf("GenerateExecuteGoCodeDescription() error = %v", err)
	}

	// Verify Run signature shows ([]mcp.Content, error)
	if !strings.Contains(got, "Run(ctx context.Context) ([]mcp.Content, error)") {
		t.Error("Description should document Run signature with ([]mcp.Content, error) return type")
	}

	// Verify multimedia content types are documented
	if !strings.Contains(got, "&mcp.TextContent{Text:") {
		t.Error("Description should document TextContent type")
	}
	if !strings.Contains(got, "&mcp.ImageContent{Data:") {
		t.Error("Description should document ImageContent type")
	}
	if !strings.Contains(got, "&mcp.AudioContent{Data:") {
		t.Error("Description should document AudioContent type")
	}

	// Verify nil, nil pattern is documented
	if !strings.Contains(got, "`nil, nil`") {
		t.Error("Description should document nil, nil return pattern")
	}
}

func TestGenerateExecuteGoCodeTool(t *testing.T) {
	tests := []struct {
		name       string
		tools      []*mcp.Tool
		maxTimeout int
		wantMax    float64
		wantErr    bool
	}{
		{
			name:       "empty tools, default timeout",
			tools:      []*mcp.Tool{},
			maxTimeout: 0,
			wantMax:    300,
		},
		{
			name: "with tools, custom timeout",
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
			maxTimeout: 600,
			wantMax:    600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := GenerateExecuteGoCodeTool(tt.tools, tt.maxTimeout)
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

			if tool.InputSchema.Type != schemaTypeObject {
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
				if codeProp.Type != schemaTypeString {
					t.Errorf("code property type = %q, want \"string\"", codeProp.Type)
				}
			}

			// Check executionTimeout property
			timeoutProp, ok := tool.InputSchema.Properties["executionTimeout"]
			if !ok {
				t.Error("InputSchema.Properties missing 'executionTimeout'")
			} else {
				if timeoutProp.Type != schemaTypeInteger {
					t.Errorf("executionTimeout property type = %q, want \"integer\"", timeoutProp.Type)
				}
				if timeoutProp.Minimum == nil || *timeoutProp.Minimum != 1 {
					t.Error("executionTimeout.Minimum should be 1")
				}
				if timeoutProp.Maximum == nil || *timeoutProp.Maximum != tt.wantMax {
					t.Errorf("executionTimeout.Maximum = %v, want %v", *timeoutProp.Maximum, tt.wantMax)
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
