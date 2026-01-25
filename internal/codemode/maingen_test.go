package codemode

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpcpe "github.com/spachava753/cpe/internal/mcp"
)

func TestGenerateMainGo(t *testing.T) {
	tests := []struct {
		name    string
		servers []ServerToolsInfo
		wantErr bool
	}{
		{
			name:    "empty servers list",
			servers: []ServerToolsInfo{},
		},
		{
			name: "single stdio server with one tool",
			servers: []ServerToolsInfo{
				{
					ServerName: "editor",
					Config: mcpcpe.ServerConfig{
						Type:    "stdio",
						Command: "editor-mcp",
						Args:    []string{"--verbose"},
					},
					Tools: []*mcp.Tool{
						{
							Name:        "read_file",
							Description: "Read a file from disk",
							InputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path": map[string]any{
										"type":        "string",
										"description": "File path",
									},
								},
							},
							OutputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"content": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "stdio server with environment variables",
			servers: []ServerToolsInfo{
				{
					ServerName: "my-server",
					Config: mcpcpe.ServerConfig{
						Type:    "stdio",
						Command: "my-mcp",
						Env: map[string]string{
							"API_KEY": "secret123",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:        "ping",
							Description: "Ping the server",
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
			},
		},
		{
			name: "http server with headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "api",
					Config: mcpcpe.ServerConfig{
						Type: "http",
						URL:  "https://api.example.com/mcp",
						Headers: map[string]string{
							"Authorization": "Bearer token123",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:         "fetch_data",
							Description:  "Fetch data from API",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
			},
		},
		{
			name: "sse server without headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "events",
					Config: mcpcpe.ServerConfig{
						Type: "sse",
						URL:  "https://events.example.com/sse",
					},
					Tools: []*mcp.Tool{
						{
							Name:        "subscribe",
							Description: "Subscribe to events",
							InputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"topic": map[string]any{"type": "string"},
								},
							},
							OutputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"id": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple http servers with different headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "api-one",
					Config: mcpcpe.ServerConfig{
						Type: "http",
						URL:  "https://one.example.com/mcp",
						Headers: map[string]string{
							"X-Api-Key": "key-one",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:         "action_one",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
				{
					ServerName: "api-two",
					Config: mcpcpe.ServerConfig{
						Type: "http",
						URL:  "https://two.example.com/mcp",
						Headers: map[string]string{
							"Authorization": "Bearer token-two",
						},
					},
					Tools: []*mcp.Tool{
						{
							Name:         "action_two",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
			},
		},
		{
			name: "server with no tools",
			servers: []ServerToolsInfo{
				{
					ServerName: "empty",
					Config: mcpcpe.ServerConfig{
						Type:    "stdio",
						Command: "empty-mcp",
					},
					Tools: []*mcp.Tool{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateMainGo(tt.servers, "/tmp/content.json")
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateMainGo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			cupaloy.SnapshotT(t, got)
		})
	}
}

func TestGenerateMainGo_DeterministicOutput(t *testing.T) {
	servers := []ServerToolsInfo{
		{
			ServerName: "zebra",
			Config: mcpcpe.ServerConfig{
				Type:    "stdio",
				Command: "zebra-mcp",
			},
			Tools: []*mcp.Tool{
				{
					Name:         "z_tool",
					InputSchema:  map[string]any{},
					OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"z": map[string]any{"type": "string"}}},
				},
			},
		},
		{
			ServerName: "alpha",
			Config: mcpcpe.ServerConfig{
				Type:    "stdio",
				Command: "alpha-mcp",
			},
			Tools: []*mcp.Tool{
				{
					Name:         "a_tool",
					InputSchema:  map[string]any{},
					OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}},
				},
			},
		},
	}

	first, err := GenerateMainGo(servers, "/tmp/content.json")
	if err != nil {
		t.Fatalf("GenerateMainGo() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		got, err := GenerateMainGo(servers, "/tmp/content.json")
		if err != nil {
			t.Fatalf("GenerateMainGo() iteration %d error = %v", i, err)
		}
		if got != first {
			t.Errorf("GenerateMainGo() output not deterministic on iteration %d\n=== FIRST ===\n%s\n=== GOT ===\n%s", i, first, got)
		}
	}
}

func TestHasInputSchema(t *testing.T) {
	tests := []struct {
		name     string
		tool     *mcp.Tool
		expected bool
	}{
		{
			name:     "nil input schema",
			tool:     &mcp.Tool{InputSchema: nil},
			expected: false,
		},
		{
			name:     "empty map input schema",
			tool:     &mcp.Tool{InputSchema: map[string]any{}},
			expected: false,
		},
		{
			name: "empty properties",
			tool: &mcp.Tool{
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			expected: false,
		},
		{
			name: "with properties",
			tool: &mcp.Tool{
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasInputSchema(tt.tool)
			if got != tt.expected {
				t.Errorf("hasInputSchema() = %v, want %v", got, tt.expected)
			}
		})
	}
}
