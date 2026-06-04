package acp

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/nalgeon/be"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

func TestRPCLoggerWrite(t *testing.T) {
	tests := []struct {
		name       string
		writes     []string
		wantAfter  []string
		wantOutput string
	}{
		{
			name: "one full frame",
			writes: []string{
				`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n",
			},
			wantAfter: []string{
				`{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","id":1,"result":{},"type":"response"}` + "\n",
			},
			wantOutput: `{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","id":1,"result":{},"type":"response"}` + "\n",
		},
		{
			name: "chunks of a frame",
			writes: []string{
				`{"jsonrpc":"2.0",`,
				`"id":2,`,
				`"method":"session/prompt"}`,
				"\n",
			},
			wantAfter: []string{
				"",
				"",
				"",
				`{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","id":2,"method":"session/prompt","type":"request"}` + "\n",
			},
			wantOutput: `{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","id":2,"method":"session/prompt","type":"request"}` + "\n",
		},
		{
			name: "multiple frames",
			writes: []string{
				`{"jsonrpc":"2.0","id":3,"result":{}}` + "\n" +
					`{"jsonrpc":"2.0","method":"session/update","params":{}}` + "\n",
			},
			wantAfter: []string{
				`{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","id":3,"result":{},"type":"response"}` + "\n" +
					`{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","method":"session/update","params":{},"type":"notification"}` + "\n",
			},
			wantOutput: `{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","id":3,"result":{},"type":"response"}` + "\n" +
				`{"level":"DEBUG","msg":"jsonrpc frame","direction":"incoming","method":"session/update","params":{},"type":"notification"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			h := slog.NewJSONHandler(&b, &slog.HandlerOptions{
				AddSource: false,
				Level:     slog.LevelDebug,
				ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
					if attr.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return attr
				},
			})
			l := &rpcLogger{
				log: slog.New(h),
				dir: "incoming",
			}

			for i, write := range tt.writes {
				n, err := l.Write([]byte(write))
				be.Err(t, err, nil)
				be.Equal(t, n, len(write))
				be.Equal(t, b.String(), tt.wantAfter[i])
			}

			be.Equal(t, b.String(), tt.wantOutput)
		})
	}
}

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
