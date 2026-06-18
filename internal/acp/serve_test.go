package acp

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"

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
		acp.StdioMcpServer("stdio", "stdio-mcp", []string{"--verbose"}, []acp.EnvVariable{
			{Name: "TOKEN", Value: "secret"},
		}),
		acp.HttpMcpServer("http", "https://example.com/mcp", []acp.HttpHeader{
			{Name: "Authorization", Value: "Bearer token"},
		}),
		acp.SseMcpServer("sse", "https://example.com/sse", []acp.HttpHeader{
			{Name: "X-Test", Value: "true"},
		}),
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
				acp.StdioMcpServer("duplicate", "client-mcp", nil, nil),
			},
		},
		{
			name: "provided name",
			provided: []acp.McpServer{
				acp.StdioMcpServer("duplicate", "first-mcp", nil, nil),
				acp.StdioMcpServer("duplicate", "second-mcp", nil, nil),
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
		acp.AcpMcpServer("client-acp", "client-acp"),
	})
	be.True(t, err != nil)
}
