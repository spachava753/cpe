package acp

import (
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

func TestMergeACPServerConfigs(t *testing.T) {
	configured := map[string]mcpconfig.ServerConfig{
		"configured": {
			Type:    "stdio",
			Command: "configured-mcp",
		},
	}

	got, err := mergeACPServerConfigs(configured, []acp.McpServer{
		{Stdio: &acp.McpServerStdio{
			Name:    "stdio",
			Command: "stdio-mcp",
			Args:    []string{"--verbose"},
			Env: []acp.EnvVariable{
				{Name: "TOKEN", Value: "secret"},
			},
		}},
		{Http: &acp.McpServerHttpInline{
			Name: "http",
			Url:  "https://example.com/mcp",
			Headers: []acp.HttpHeader{
				{Name: "Authorization", Value: "Bearer token"},
			},
		}},
		{Sse: &acp.McpServerSseInline{
			Name: "sse",
			Url:  "https://example.com/sse",
			Headers: []acp.HttpHeader{
				{Name: "X-Test", Value: "true"},
			},
		}},
	})
	be.Err(t, err, nil)

	be.Equal(t, got["configured"], configured["configured"])
	be.Equal(t, got["stdio"], mcpconfig.ServerConfig{
		Type:    "stdio",
		Command: "stdio-mcp",
		Args:    []string{"--verbose"},
		Env: map[string]string{
			"TOKEN": "secret",
		},
	})
	be.Equal(t, got["http"], mcpconfig.ServerConfig{
		Type: "http",
		URL:  "https://example.com/mcp",
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
	})
	be.Equal(t, got["sse"], mcpconfig.ServerConfig{
		Type: "sse",
		URL:  "https://example.com/sse",
		Headers: map[string]string{
			"X-Test": "true",
		},
	})
}

func TestMergeACPServerConfigsRejectsCollisions(t *testing.T) {
	tests := []struct {
		name       string
		configured map[string]mcpconfig.ServerConfig
		provided   []acp.McpServer
	}{
		{
			name: "configured name",
			configured: map[string]mcpconfig.ServerConfig{
				"duplicate": {Type: "stdio", Command: "configured-mcp"},
			},
			provided: []acp.McpServer{
				{Stdio: &acp.McpServerStdio{Name: "duplicate", Command: "client-mcp"}},
			},
		},
		{
			name: "provided name",
			provided: []acp.McpServer{
				{Stdio: &acp.McpServerStdio{Name: "duplicate", Command: "first-mcp"}},
				{Stdio: &acp.McpServerStdio{Name: "duplicate", Command: "second-mcp"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mergeACPServerConfigs(tt.configured, tt.provided)
			be.True(t, err != nil)
		})
	}
}

func TestMergeACPServerConfigsRejectsUnsupportedACPTransport(t *testing.T) {
	_, err := mergeACPServerConfigs(nil, []acp.McpServer{
		{Acp: &acp.McpServerAcpInline{Name: "client-acp"}},
	})
	be.True(t, err != nil)
}
