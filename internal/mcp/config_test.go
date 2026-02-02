package mcp

import (
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/go-playground/validator/v10"
)

func TestServerConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config ServerConfig
	}{
		{
			name: "Valid stdio config",
			config: ServerConfig{
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			},
		},
		{
			name: "Url with stdio",
			config: ServerConfig{
				Type:    "stdio",
				URL:     "http://localhost:3000",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			},
		},
		{
			name: "Valid sse config",
			config: ServerConfig{
				Type: "sse",
				URL:  "http://localhost:3000",
			},
		},
		{
			name: "Valid http config",
			config: ServerConfig{
				Type: "http",
				URL:  "http://localhost:3000",
			},
		},
		{
			name: "Invalid url",
			config: ServerConfig{
				Type: "sse",
				URL:  "s3://test/file",
			},
		},
		{
			name: "Valid with timeout and env",
			config: ServerConfig{
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
				Timeout: 30,
				Env: map[string]string{
					"NODE_ENV": "production",
				},
			},
		},
		{
			name: "Missing URL for sse",
			config: ServerConfig{
				Type: "sse",
			},
		},
		{
			name: "Missing command for stdio",
			config: ServerConfig{
				Type: "stdio",
				Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			},
		},
		{
			name: "Invalid type",
			config: ServerConfig{
				Type:    "invalid",
				Command: "npx",
			},
		},
		{
			name: "Negative timeout",
			config: ServerConfig{
				Type:    "stdio",
				Command: "npx",
				Timeout: -10,
			},
		},
		{
			name: "Env vars on non-stdio server",
			config: ServerConfig{
				Type: "sse",
				URL:  "http://localhost:3000",
				Env: map[string]string{
					"NODE_ENV": "production",
				},
			},
		},
		{
			name: "Blacklist mode with DisabledTools",
			config: ServerConfig{
				Type:          "stdio",
				Command:       "editor-mcp",
				DisabledTools: []string{"shell"},
			},
		},
		{
			name: "Whitelist mode with EnabledTools",
			config: ServerConfig{
				Type:         "stdio",
				Command:      "editor-mcp",
				EnabledTools: []string{"shell"},
			},
		},
		{
			name: "EnabledTools and DisabledTools mutually exclusive",
			config: ServerConfig{
				Type:          "stdio",
				Command:       "editor-mcp",
				EnabledTools:  []string{"shell"},
				DisabledTools: []string{"str_replace"},
			},
		},
		{
			name: "Empty EnabledTools rejected",
			config: ServerConfig{
				Type:         "stdio",
				Command:      "editor-mcp",
				EnabledTools: []string{},
			},
		},
		{
			name: "Empty DisabledTools rejected",
			config: ServerConfig{
				Type:          "stdio",
				Command:       "editor-mcp",
				DisabledTools: []string{},
			},
		},
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validate.Struct(tc.config)
			var result string
			if err != nil {
				result = err.Error()
			} else {
				result = "<nil>"
			}
			cupaloy.SnapshotT(t, result)
		})
	}
}
