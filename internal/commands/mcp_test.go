package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"

	"github.com/spachava753/cpe/internal/config"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
)

func TestMCPListServers(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr bool
	}{
		{
			name: "no servers configured",
			config: &config.Config{
				MCPServers: nil,
			},
			wantErr: false,
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

			cupaloy.SnapshotT(t, buf.String())
		})
	}
}

// noopRenderer is a test renderer that passes through text unchanged
type noopRenderer struct{}

func (r *noopRenderer) Render(in string) (string, error) {
	return in, nil
}

func TestMCPCodeDesc(t *testing.T) {
	tests := []struct {
		name      string
		servers   map[string]mcpinternal.ServerConfig
		codeMode  *config.CodeModeConfig
		wantErr   bool
		checkFunc func(t *testing.T, output string)
	}{
		{
			name:     "no servers - empty tools",
			servers:  nil,
			codeMode: &config.CodeModeConfig{Enabled: true},
			wantErr:  false,
			checkFunc: func(t *testing.T, output string) {
				if !strings.Contains(output, "execute_go_code Tool Description") {
					t.Error("expected header in output")
				}
				if !strings.Contains(output, "**Code mode tools:** 0") {
					t.Error("expected zero code mode tools count")
				}
			},
		},
		{
			name:     "code mode disabled shows note",
			servers:  nil,
			codeMode: nil,
			wantErr:  false,
			checkFunc: func(t *testing.T, output string) {
				if !strings.Contains(output, "Code mode is not enabled") {
					t.Error("expected 'not enabled' note in output")
				}
			},
		},
		{
			name:    "excluded tools shown in output",
			servers: nil,
			codeMode: &config.CodeModeConfig{
				Enabled:       true,
				ExcludedTools: []string{"tool1", "tool2"},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				if !strings.Contains(output, "Excluded tools:") {
					t.Error("expected excluded tools in output")
				}
				if !strings.Contains(output, "tool1") || !strings.Contains(output, "tool2") {
					t.Error("expected excluded tool names in output")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := MCPCodeDescOptions{
				MCPServers: tt.servers,
				CodeMode:   tt.codeMode,
				Writer:     &buf,
				Renderer:   &noopRenderer{},
			}

			err := MCPCodeDesc(context.Background(), opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("MCPCodeDesc() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, buf.String())
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
