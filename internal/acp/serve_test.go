package acp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nalgeon/be"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	cpemcp "github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/mcpconfig"
)

type testToolCallingGenerator struct{}

func (testToolCallingGenerator) Generate(context.Context, gai.Dialog, *gai.GenOpts) (gai.Response, error) {
	return gai.Response{}, nil
}

func (testToolCallingGenerator) Register(gai.Tool) error {
	return nil
}

func TestServerRuntimeCreatorRuntimeContextOutlivesCreateContext(t *testing.T) {
	originalGenerator := initializeGeneratorFromModel
	originalMCP := initializeMCPConnections
	var generatorCtx context.Context
	var runtimeCtx context.Context
	initializeGeneratorFromModel = func(ctx context.Context, _ config.Model, _ string, _ time.Duration) (gai.Generator, error) {
		generatorCtx = ctx
		return testToolCallingGenerator{}, nil
	}
	initializeMCPConnections = func(ctx context.Context, servers map[string]mcpconfig.ServerConfig) (*cpemcp.MCPState, error) {
		runtimeCtx = ctx
		return cpemcp.NewMCPState(), nil
	}
	t.Cleanup(func() {
		initializeGeneratorFromModel = originalGenerator
		initializeMCPConnections = originalMCP
	})

	creator := &serverRuntimeCreator{
		rawCfg: &config.RawConfig{
			Models: []config.ModelConfig{
				{
					Model: config.Model{
						Ref:           "test-model",
						DisplayName:   "Test Model",
						ID:            "test-model",
						Type:          "responses",
						ContextWindow: 100,
						MaxOutput:     10,
					},
					DisableEditTool: true,
					MCPServers: map[string]mcpconfig.ServerConfig{
						"stub": {Type: "stdio", Command: "stub-mcp"},
					},
				},
			},
		},
		stderr: io.Discard,
	}
	createCtx, cancelCreate := context.WithCancel(t.Context())
	runtime, err := creator.Create(createCtx, session{
		id:    "session-1",
		model: "test-model",
	}, acp.ClientCapabilities{})
	be.Err(t, err, nil)
	if generatorCtx == nil {
		t.Fatal("generator initializer was not called")
	}
	if runtimeCtx == nil {
		t.Fatal("MCP initializer was not called")
	}

	cancelCreate()
	select {
	case <-generatorCtx.Done():
		t.Fatal("generator context was cancelled by create context after successful creation")
	default:
	}
	select {
	case <-runtimeCtx.Done():
		t.Fatal("MCP context was cancelled by create context after successful creation")
	default:
	}

	be.Err(t, runtime.Close(), nil)
	select {
	case <-generatorCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for generator context cancellation on close")
	}
	select {
	case <-runtimeCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for MCP context cancellation on close")
	}
}

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
