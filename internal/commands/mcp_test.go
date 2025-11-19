package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/config"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
)

func TestMCPListServers(t *testing.T) {
	tests := []struct {
		name               string
		config             *config.Config
		wantErr            bool
		wantOutputContains []string
	}{
		{
			name: "no servers configured",
			config: &config.Config{
				MCPServers: nil,
			},
			wantErr:            false,
			wantOutputContains: []string{"No MCP servers configured."},
		},
		{
			name: "single server configured",
			config: &config.Config{
				MCPServers: map[string]mcpinternal.ServerConfig{
					"test-server": {
						Type:    "stdio",
						Command: "node",
						Args:    []string{"server.js"},
						Timeout: 30,
					},
				},
			},
			wantErr: false,
			wantOutputContains: []string{
				"Configured MCP Servers:",
				"test-server",
				"Type: stdio",
				"Timeout: 30s",
				"Command: node server.js",
			},
		},
		{
			name: "multiple servers with different types",
			config: &config.Config{
				MCPServers: map[string]mcpinternal.ServerConfig{
					"stdio-server": {
						Type:    "stdio",
						Command: "python",
						Args:    []string{"server.py"},
					},
					"sse-server": {
						Type: "sse",
						URL:  "http://localhost:8080",
					},
				},
			},
			wantErr: false,
			wantOutputContains: []string{
				"Configured MCP Servers:",
				"stdio-server",
				"sse-server",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := MCPListServersOptions{
				MCPServers: tt.config.MCPServers,
				Writer:     &buf,
			}

			err := MCPListServers(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("MCPListServers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			output := buf.String()
			for _, want := range tt.wantOutputContains {
				if !strings.Contains(output, want) {
					t.Errorf("MCPListServers() output does not contain %q\nOutput: %s", want, output)
				}
			}
		})
	}
}

func TestMCPInfo(t *testing.T) {
	tests := []struct {
		name       string
		config     *config.Config
		serverName string
		wantErr    bool
		errMsg     string
	}{
		{
			name: "server not found",
			config: &config.Config{
				MCPServers: map[string]mcpinternal.ServerConfig{
					"existing-server": {
						Type:    "stdio",
						Command: "node",
					},
				},
			},
			serverName: "nonexistent-server",
			wantErr:    true,
			errMsg:     "server 'nonexistent-server' not found",
		},
		{
			name: "no servers configured",
			config: &config.Config{
				MCPServers: nil,
			},
			serverName: "any-server",
			wantErr:    true,
			errMsg:     "no MCP servers configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := MCPInfoOptions{
				MCPServers: tt.config.MCPServers,
				ServerName: tt.serverName,
				Writer:     &buf,
			}

			err := MCPInfo(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("MCPInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("MCPInfo() error = %v, want error containing %q", err, tt.errMsg)
			}
		})
	}
}
