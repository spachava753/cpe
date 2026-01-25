package codemode

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGenerateToolDefinitions(t *testing.T) {
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
			name: "tool with input and output schemas (get_weather from spec)",
			tools: []*mcp.Tool{
				{
					Name:        "get_weather",
					Description: "Get current weather data for a location",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{
								"type":        "string",
								"description": "The name of the city to get weather for",
							},
							"unit": map[string]any{
								"type":        "string",
								"enum":        []any{"celsius", "fahrenheit"},
								"description": "Temperature unit for the weather response",
							},
						},
						"required": []any{"city", "unit"},
					},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"temperature": map[string]any{
								"type":        "number",
								"description": "Temperature in celsius",
							},
						},
						"required": []any{"temperature"},
					},
				},
			},
		},
		{
			name: "tool with no input schema (get_city)",
			tools: []*mcp.Tool{
				{
					Name:        "get_city",
					Description: "Get current city location",
					InputSchema: map[string]any{},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{
								"type":        "string",
								"description": "Current city location",
							},
						},
						"required": []any{"city"},
					},
				},
			},
		},
		{
			name: "tool with no output schema",
			tools: []*mcp.Tool{
				{
					Name:        "send_message",
					Description: "Send a message",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{
								"type": "string",
							},
						},
					},
					OutputSchema: nil,
				},
			},
		},
		{
			name: "tool with no description",
			tools: []*mcp.Tool{
				{
					Name:        "ping",
					Description: "",
					InputSchema: map[string]any{},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"pong": map[string]any{"type": "boolean"},
						},
					},
				},
			},
		},
		{
			name: "multiple tools sorted by name",
			tools: []*mcp.Tool{
				{
					Name:        "zebra_tool",
					Description: "Z tool",
					InputSchema: map[string]any{},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"z": map[string]any{"type": "string"},
						},
					},
				},
				{
					Name:        "alpha_tool",
					Description: "A tool",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"a": map[string]any{"type": "string"},
						},
					},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"result": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		{
			name: "tool with multiline description",
			tools: []*mcp.Tool{
				{
					Name:        "multi_line_tool",
					Description: "First line of description.\nSecond line of description.\nThird line of description.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"param": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		{
			name: "tool with nil input schema",
			tools: []*mcp.Tool{
				{
					Name:         "get_time",
					Description:  "Get current time",
					InputSchema:  nil,
					OutputSchema: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateToolDefinitions(tt.tools)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateToolDefinitions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestConvertSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  any
		wantNil bool
		wantErr bool
	}{
		{
			name:    "nil schema",
			schema:  nil,
			wantNil: true,
		},
		{
			name:    "empty object schema",
			schema:  map[string]any{},
			wantNil: true,
		},
		{
			name: "valid object schema",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertSchema(tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("convertSchema() gotNil = %v, wantNil %v", got == nil, tt.wantNil)
			}
		})
	}
}
