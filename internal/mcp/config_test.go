package mcp

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestServerConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
	}{
		{
			name: "Valid stdio config",
			config: ServerConfig{
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			},
			wantErr: false,
		},
		{
			name: "Url with stdio",
			config: ServerConfig{
				Type:    "stdio",
				URL:     "http://localhost:3000",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			},
			wantErr: true,
		},
		{
			name: "Valid sse config",
			config: ServerConfig{
				Type: "sse",
				URL:  "http://localhost:3000",
			},
			wantErr: false,
		},
		{
			name: "Valid http config",
			config: ServerConfig{
				Type: "http",
				URL:  "http://localhost:3000",
			},
			wantErr: false,
		},
		{
			name: "Invalid url",
			config: ServerConfig{
				Type: "sse",
				URL:  "s3://test/file",
			},
			wantErr: true,
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
			wantErr: false,
		},
		{
			name: "Missing URL for sse",
			config: ServerConfig{
				Type: "sse",
			},
			wantErr: true,
		},
		{
			name: "Missing command for stdio",
			config: ServerConfig{
				Type: "stdio",
				Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			},
			wantErr: true,
		},
		{
			name: "Invalid type",
			config: ServerConfig{
				Type:    "invalid",
				Command: "npx",
			},
			wantErr: true,
		},
		{
			name: "Negative timeout",
			config: ServerConfig{
				Type:    "stdio",
				Command: "npx",
				Timeout: -10,
			},
			wantErr: true,
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
			wantErr: true,
		},
		{
			name: "Blacklist mode with DisabledTools",
			config: ServerConfig{
				Type:          "stdio",
				Command:       "editor-mcp",
				DisabledTools: []string{"shell"},
			},
			wantErr: false,
		},
		{
			name: "Whitelist mode with EnabledTools",
			config: ServerConfig{
				Type:         "stdio",
				Command:      "editor-mcp",
				EnabledTools: []string{"shell"},
			},
			wantErr: false,
		},
		{
			name: "EnabledTools and DisabledTools mutually exclusive",
			config: ServerConfig{
				Type:          "stdio",
				Command:       "editor-mcp",
				EnabledTools:  []string{"shell"},
				DisabledTools: []string{"str_replace"},
			},
			wantErr: true,
		},
		{
			name: "Empty EnabledTools rejected",
			config: ServerConfig{
				Type:         "stdio",
				Command:      "editor-mcp",
				EnabledTools: []string{},
			},
			wantErr: true,
		},
		{
			name: "Empty DisabledTools rejected",
			config: ServerConfig{
				Type:          "stdio",
				Command:       "editor-mcp",
				DisabledTools: []string{},
			},
			wantErr: true,
		},
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validate.Struct(tc.config)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
