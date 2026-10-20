package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/plugins"
)

func TestRegistryLifecycle(t *testing.T) {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "fixture",
			Version: "1",
		},
		&mcp.ServerOptions{
			Instructions: "fixture instructions",
		},
	)
	calls := 0
	server.AddTool(
		&mcp.Tool{
			Name:        "echo",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls++
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: "retained",
					},
				},
				IsError: true,
			}, nil
		},
	)
	connections := 0
	var remote *mcp.ServerSession
	registry, err := NewRegistry(
		func(ctx context.Context, _ plugins.MCPServer) (*mcp.ClientSession, error) {
			connections++
			a, b := mcp.NewInMemoryTransports()
			var err error
			remote, err = server.Connect(
				ctx,
				a,
				nil,
			)
			if err != nil {
				return nil, err
			}
			t.Cleanup(func() { _ = remote.Close() })
			return mcp.NewClient(
				&mcp.Implementation{
					Name:    "fixture",
					Version: "1",
				},
				nil,
			).Connect(
				ctx,
				b,
				nil,
			)
		},
		map[ServerID]plugins.MCPServer{
			"fixture": {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if connections != 0 || registry.Instructions("fixture") != "" {
		t.Fatal("constructor opened a connection")
	}
	for range 2 {
		tools, err := registry.Load(
			t.Context(),
			"fixture",
		)
		if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
			t.Fatalf(
				"tool discovery: %v, %v",
				tools,
				err,
			)
		}
	}
	if connections != 1 || calls != 0 || registry.Instructions("fixture") != "fixture instructions" {
		t.Fatal("discovery did not reuse the connection or executed a tool")
	}
	result, err := registry.Call(
		t.Context(),
		"fixture",
		&mcp.CallToolParams{
			Name: "echo",
		},
	)
	if err != nil || !result.IsError || len(result.Content) != 1 || calls != 1 {
		t.Fatalf(
			"tool result was not retained: %v, %v",
			result,
			err,
		)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if err := remote.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Load(
		t.Context(),
		"fixture",
	); err == nil || connections != 1 {
		t.Fatal("closed registry reconnected")
	}
}

func TestRegistryFailures(t *testing.T) {
	for _, scenario := range []string{
		"missing connector",
		"unknown server",
		"cancelled setup",
		"connection failure",
		"missing tool name",
	} {
		t.Run(
			scenario,
			func(t *testing.T) {
				calls := 0
				connector := Connector(func(context.Context, plugins.MCPServer) (*mcp.ClientSession, error) {
					calls++
					return nil, errors.New("fixture connection failed")
				})
				if scenario == "missing connector" {
					connector = nil
				}
				registry, err := NewRegistry(
					connector,
					map[ServerID]plugins.MCPServer{
						"fixture": {},
					},
				)
				if scenario == "missing connector" {
					if err == nil {
						t.Fatal("accepted missing connector")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer registry.Close()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				id := ServerID("fixture")
				switch scenario {
				case "unknown server":
					id = "unknown"
				case "cancelled setup":
					cancel()
				}
				if scenario == "missing tool name" {
					_, err = registry.Call(
						ctx,
						id,
						&mcp.CallToolParams{},
					)
				} else {
					_, err = registry.Load(
						ctx,
						id,
					)
				}
				wantCalls := 0
				if scenario == "connection failure" {
					wantCalls = 1
				}
				if err == nil || calls != wantCalls {
					t.Fatalf(
						"error=%v, connector calls=%d",
						err,
						calls,
					)
				}
			},
		)
	}
}
