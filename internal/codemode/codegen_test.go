package codemode

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGenerateToolDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		tools   []*mcp.Tool
		want    string
		wantErr bool
	}{
		{
			name:  "empty tools list",
			tools: []*mcp.Tool{},
			want:  "",
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
			want: `type GetWeatherInput struct {
	// City The name of the city to get weather for
	City string ` + "`json:\"city\"`" + `
	// Unit Temperature unit for the weather response
	// Must be one of "celsius", "fahrenheit"
	Unit string ` + "`json:\"unit\"`" + `
}

type GetWeatherOutput struct {
	// Temperature Temperature in celsius
	Temperature float64 ` + "`json:\"temperature\"`" + `
}

// GetWeather Get current weather data for a location
var GetWeather func(ctx context.Context, input GetWeatherInput) (GetWeatherOutput, error)`,
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
			want: `type GetCityOutput struct {
	// City Current city location
	City string ` + "`json:\"city\"`" + `
}

// GetCity Get current city location
var GetCity func(ctx context.Context) (GetCityOutput, error)`,
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
			want: `type SendMessageInput struct {
	Text *string ` + "`json:\"text,omitempty\"`" + `
}

type SendMessageOutput = string

// SendMessage Send a message
var SendMessage func(ctx context.Context, input SendMessageInput) (SendMessageOutput, error)`,
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
			want: `type PingOutput struct {
	Pong *bool ` + "`json:\"pong,omitempty\"`" + `
}

var Ping func(ctx context.Context) (PingOutput, error)`,
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
			want: `type AlphaToolInput struct {
	A *string ` + "`json:\"a,omitempty\"`" + `
}

type AlphaToolOutput struct {
	Result *string ` + "`json:\"result,omitempty\"`" + `
}

type ZebraToolOutput struct {
	Z *string ` + "`json:\"z,omitempty\"`" + `
}

// AlphaTool A tool
var AlphaTool func(ctx context.Context, input AlphaToolInput) (AlphaToolOutput, error)

// ZebraTool Z tool
var ZebraTool func(ctx context.Context) (ZebraToolOutput, error)`,
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
			want: `type GetTimeOutput = string

// GetTime Get current time
var GetTime func(ctx context.Context) (GetTimeOutput, error)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateToolDefinitions(tt.tools)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateToolDefinitions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateToolDefinitions() mismatch\ngot:\n%s\n\nwant:\n%s", got, tt.want)
			}
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
