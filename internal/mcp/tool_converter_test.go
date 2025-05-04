package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spachava753/gai"
	"github.com/stretchr/testify/assert"
)

func TestConvertMCPToolToGAITool(t *testing.T) {
	tests := []struct {
		name     string
		mcpTool  mcp.Tool
		wantTool gai.Tool
		wantErr  bool
	}{
		{
			name: "Simple tool without parameters",
			mcpTool: mcp.Tool{
				Name:        "simple_tool",
				Description: "A simple tool without parameters",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
				},
			},
			wantTool: gai.Tool{
				Name:        "simple_tool",
				Description: "A simple tool without parameters",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with string parameter",
			mcpTool: mcp.Tool{
				Name:        "string_param_tool",
				Description: "A tool with a string parameter",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "A text parameter",
						},
					},
				},
			},
			wantTool: gai.Tool{
				Name:        "string_param_tool",
				Description: "A tool with a string parameter",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"text": {
							Type:        gai.String,
							Description: "A text parameter",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with required parameters",
			mcpTool: mcp.Tool{
				Name:        "required_param_tool",
				Description: "A tool with required parameters",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "File path",
						},
						"flag": map[string]interface{}{
							"type":        "boolean",
							"description": "A flag parameter",
						},
					},
					Required: []string{"path"},
				},
			},
			wantTool: gai.Tool{
				Name:        "required_param_tool",
				Description: "A tool with required parameters",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"path": {
							Type:        gai.String,
							Description: "File path",
						},
						"flag": {
							Type:        gai.Boolean,
							Description: "A flag parameter",
						},
					},
					Required: []string{"path"},
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with multiple parameter types",
			mcpTool: mcp.Tool{
				Name:        "multi_type_tool",
				Description: "A tool with multiple parameter types",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "A text parameter",
						},
						"number": map[string]interface{}{
							"type":        "number",
							"description": "A number parameter",
						},
						"integer": map[string]interface{}{
							"type":        "integer",
							"description": "An integer parameter",
						},
						"flag": map[string]interface{}{
							"type":        "boolean",
							"description": "A boolean parameter",
						},
					},
					Required: []string{"text", "number"},
				},
			},
			wantTool: gai.Tool{
				Name:        "multi_type_tool",
				Description: "A tool with multiple parameter types",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"text": {
							Type:        gai.String,
							Description: "A text parameter",
						},
						"number": {
							Type:        gai.Number,
							Description: "A number parameter",
						},
						"integer": {
							Type:        gai.Integer,
							Description: "An integer parameter",
						},
						"flag": {
							Type:        gai.Boolean,
							Description: "A boolean parameter",
						},
					},
					Required: []string{"text", "number"},
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with nested object parameters",
			mcpTool: mcp.Tool{
				Name:        "nested_object_tool",
				Description: "A tool with nested object parameters",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"config": map[string]interface{}{
							"type":        "object",
							"description": "A config object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "Configuration name",
								},
								"enabled": map[string]interface{}{
									"type":        "boolean",
									"description": "Whether the configuration is enabled",
								},
							},
							"required": []string{"name"},
						},
					},
					Required: []string{"config"},
				},
			},
			wantTool: gai.Tool{
				Name:        "nested_object_tool",
				Description: "A tool with nested object parameters",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"config": {
							Type:        gai.Object,
							Description: "A config object",
							Properties: map[string]gai.Property{
								"name": {
									Type:        gai.String,
									Description: "Configuration name",
								},
								"enabled": {
									Type:        gai.Boolean,
									Description: "Whether the configuration is enabled",
								},
							},
							Required: []string{"name"},
						},
					},
					Required: []string{"config"},
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with array parameters",
			mcpTool: mcp.Tool{
				Name:        "array_tool",
				Description: "A tool with array parameters",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"tags": map[string]interface{}{
							"type":        "array",
							"description": "List of tags",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
						"coordinates": map[string]interface{}{
							"type":        "array",
							"description": "Coordinate pairs",
							"items": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "number",
								},
							},
						},
					},
				},
			},
			wantTool: gai.Tool{
				Name:        "array_tool",
				Description: "A tool with array parameters",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"tags": {
							Type:        gai.Array,
							Description: "List of tags",
							Items: &gai.Property{
								Type: gai.String,
							},
						},
						"coordinates": {
							Type:        gai.Array,
							Description: "Coordinate pairs",
							Items: &gai.Property{
								Type: gai.Array,
								Items: &gai.Property{
									Type: gai.Number,
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with enumeration values",
			mcpTool: mcp.Tool{
				Name:        "enum_tool",
				Description: "A tool with enumeration parameters",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"color": map[string]interface{}{
							"type":        "string",
							"description": "Color selection",
							"enum":        []interface{}{"red", "green", "blue"},
						},
						"size": map[string]interface{}{
							"type":        "string",
							"description": "Size selection",
							"enum":        []interface{}{"small", "medium", "large"},
						},
					},
				},
			},
			wantTool: gai.Tool{
				Name:        "enum_tool",
				Description: "A tool with enumeration parameters",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"color": {
							Type:        gai.String,
							Description: "Color selection",
							Enum:        []string{"red", "green", "blue"},
						},
						"size": {
							Type:        gai.String,
							Description: "Size selection",
							Enum:        []string{"small", "medium", "large"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Tool with deep nesting and mixed types",
			mcpTool: mcp.Tool{
				Name:        "complex_tool",
				Description: "A tool with complex nested structure",
				InputSchema: mcp.ToolInputSchema{
					Type: "object",
					Properties: map[string]interface{}{
						"user": map[string]interface{}{
							"type":        "object",
							"description": "User information",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{
									"type":        "string",
									"description": "User's name",
								},
								"age": map[string]interface{}{
									"type":        "integer",
									"description": "User's age",
								},
								"addresses": map[string]interface{}{
									"type":        "array",
									"description": "User's addresses",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"street": map[string]interface{}{
												"type":        "string",
												"description": "Street address",
											},
											"city": map[string]interface{}{
												"type":        "string",
												"description": "City",
											},
											"zip": map[string]interface{}{
												"type":        "string",
												"description": "Zip code",
											},
											"isPrimary": map[string]interface{}{
												"type":        "boolean",
												"description": "Whether this is the primary address",
											},
										},
										"required": []string{"street", "city"},
									},
								},
							},
							"required": []string{"name"},
						},
						"preferences": map[string]interface{}{
							"type":        "object",
							"description": "User preferences",
							"properties": map[string]interface{}{
								"theme": map[string]interface{}{
									"type":        "string",
									"description": "UI theme",
									"enum":        []interface{}{"light", "dark", "system"},
								},
								"notifications": map[string]interface{}{
									"type":        "boolean",
									"description": "Whether notifications are enabled",
								},
							},
						},
					},
					Required: []string{"user"},
				},
			},
			wantTool: gai.Tool{
				Name:        "complex_tool",
				Description: "A tool with complex nested structure",
				InputSchema: gai.InputSchema{
					Type: gai.Object,
					Properties: map[string]gai.Property{
						"user": {
							Type:        gai.Object,
							Description: "User information",
							Properties: map[string]gai.Property{
								"name": {
									Type:        gai.String,
									Description: "User's name",
								},
								"age": {
									Type:        gai.Integer,
									Description: "User's age",
								},
								"addresses": {
									Type:        gai.Array,
									Description: "User's addresses",
									Items: &gai.Property{
										Type: gai.Object,
										Properties: map[string]gai.Property{
											"street": {
												Type:        gai.String,
												Description: "Street address",
											},
											"city": {
												Type:        gai.String,
												Description: "City",
											},
											"zip": {
												Type:        gai.String,
												Description: "Zip code",
											},
											"isPrimary": {
												Type:        gai.Boolean,
												Description: "Whether this is the primary address",
											},
										},
										Required: []string{"street", "city"},
									},
								},
							},
							Required: []string{"name"},
						},
						"preferences": {
							Type:        gai.Object,
							Description: "User preferences",
							Properties: map[string]gai.Property{
								"theme": {
									Type:        gai.String,
									Description: "UI theme",
									Enum:        []string{"light", "dark", "system"},
								},
								"notifications": {
									Type:        gai.Boolean,
									Description: "Whether notifications are enabled",
								},
							},
						},
					},
					Required: []string{"user"},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid schema type",
			mcpTool: mcp.Tool{
				Name:        "invalid_schema_tool",
				Description: "A tool with an invalid schema",
				InputSchema: mcp.ToolInputSchema{
					Type: "invalid_type",
				},
			},
			wantTool: gai.Tool{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTool, err := ConvertMCPToolToGAITool(tt.mcpTool)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTool.Name, gotTool.Name)
				assert.Equal(t, tt.wantTool.Description, gotTool.Description)

				// In the stub implementation, we're not fully converting the schema yet,
				// so we'll only check the Name and Description in the tests
				// Once you implement the full conversion, you can uncomment this
				assert.Equal(t, tt.wantTool.InputSchema, gotTool.InputSchema)
			}
		})
	}
}
