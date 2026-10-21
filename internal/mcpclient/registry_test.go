package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
		func(ctx context.Context, _ plugins.MCPServer, toolsChanged func()) (*mcp.ClientSession, error) {
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
				&mcp.ClientOptions{
					ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { toolsChanged() },
				},
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
				connector := Connector(func(context.Context, plugins.MCPServer, func()) (*mcp.ClientSession, error) {
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

func TestRegistryToolChanges(t *testing.T) {
	for _, transport := range []plugins.Transport{
		plugins.StreamableHTTP,
		plugins.SSE,
	} {
		t.Run(
			string(transport),
			func(t *testing.T) {
				ctx, cancel := context.WithTimeout(
					t.Context(),
					10*time.Second,
				)
				defer cancel()
				changed := make(chan struct{}, 16)
				waitChanged := func(ctx context.Context) error {
					select {
					case <-changed:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				server := mcp.NewServer(
					&mcp.Implementation{
						Name:    "fixture",
						Version: "1",
					},
					&mcp.ServerOptions{
						PageSize: 1,
					},
				)
				handler := func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				}
				server.AddTool(
					&mcp.Tool{
						Name:        "obsolete",
						InputSchema: json.RawMessage(`{"type":"object"}`),
					},
					handler,
				)
				server.AddTool(
					&mcp.Tool{
						Name:        "change",
						InputSchema: json.RawMessage(`{"type":"object"}`),
					},
					func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						// The client holds its registry lock throughout this call. Notification
						// handling must still finish before the tool response can be sent.
						server.RemoveTools("obsolete")
						if err := waitChanged(ctx); err != nil {
							return nil, err
						}
						server.AddTool(
							&mcp.Tool{
								Name:        "change",
								Description: "updated metadata",
								InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
							},
							handler,
						)
						if err := waitChanged(ctx); err != nil {
							return nil, err
						}
						server.AddTool(
							&mcp.Tool{
								Name:        "new",
								InputSchema: json.RawMessage(`{"type":"object"}`),
							},
							handler,
						)
						return &mcp.CallToolResult{}, waitChanged(ctx)
					},
				)
				var lists atomic.Int32
				var failList, changeDuringList atomic.Bool
				server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
					return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
						if method != "tools/list" {
							return next(
								ctx,
								method,
								request,
							)
						}
						lists.Add(1)
						if failList.Swap(false) {
							return nil, errors.New("discovery unavailable")
						}
						result, err := next(
							ctx,
							method,
							request,
						)
						if err == nil && changeDuringList.Swap(false) {
							server.RemoveTools("new")
							if err := waitChanged(ctx); err != nil {
								return nil, err
							}
						}
						return result, err
					}
				})
				getServer := func(*http.Request) *mcp.Server { return server }
				var httpHandler http.Handler = mcp.NewStreamableHTTPHandler(
					getServer,
					nil,
				)
				if transport == plugins.SSE {
					httpHandler = mcp.NewSSEHandler(
						getServer,
						nil,
					)
				}
				httpServer := httptest.NewServer(httpHandler)
				defer httpServer.Close()
				endpoint, err := url.Parse(httpServer.URL)
				if err != nil {
					t.Fatal(err)
				}
				connections := 0
				registry, err := NewRegistry(
					func(ctx context.Context, config plugins.MCPServer, toolsChanged func()) (*mcp.ClientSession, error) {
						connections++
						return Connect(
							ctx,
							config,
							func() {
								toolsChanged()
								changed <- struct{}{}
							},
						)
					},
					map[ServerID]plugins.MCPServer{
						"fixture": {
							Type: transport,
							URL:  *endpoint,
						},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				defer registry.Close()
				for range 2 {
					tools, err := registry.Load(
						ctx,
						"fixture",
					)
					if err != nil || len(tools) != 2 || tools[1].Name != "obsolete" {
						t.Fatalf(
							"initial tools: %v, %v",
							tools,
							err,
						)
					}
				}
				if lists.Load() != 2 {
					t.Fatal("unchanged tool list was not cached across paginated loads")
				}
				if result, err := registry.Call(
					ctx,
					"fixture",
					&mcp.CallToolParams{
						Name: "change",
					},
				); err != nil || result.IsError {
					t.Fatalf(
						"change tool: %v, %v",
						result,
						err,
					)
				}
				failList.Store(true)
				if _, err := registry.Load(
					ctx,
					"fixture",
				); err == nil {
					t.Fatal("refresh failure silently returned stale tools")
				}
				// A notification during a paginated refresh must not be lost when the
				// response arrives. The following load must fetch again.
				changeDuringList.Store(true)
				if _, err := registry.Load(
					ctx,
					"fixture",
				); err != nil {
					t.Fatal(err)
				}
				before := lists.Load()
				tools, err := registry.Load(
					ctx,
					"fixture",
				)
				if lists.Load() != before+1 {
					t.Fatal("notification during discovery was lost")
				}
				if err != nil || len(tools) != 1 || tools[0].Name != "change" || tools[0].Description != "updated metadata" {
					t.Fatalf(
						"refreshed tools: %v, %v",
						tools,
						err,
					)
				}
				schema, err := json.Marshal(tools[0].InputSchema)
				if err != nil || !strings.Contains(
					string(schema),
					`"query"`,
				) {
					t.Fatalf(
						"schema was not refreshed: %s, %v",
						schema,
						err,
					)
				}
				count := lists.Load()
				if _, err := registry.Load(
					ctx,
					"fixture",
				); err != nil || lists.Load() != count || connections != 1 {
					t.Fatal("refreshed cache or connection was not reused")
				}
				server.RemoveTools("change")
				if err := waitChanged(ctx); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					tools, err := registry.Load(
						ctx,
						"fixture",
					)
					if err != nil || len(tools) != 0 {
						t.Fatalf(
							"empty tool list: %v, %v",
							tools,
							err,
						)
					}
				}
				if lists.Load() != count+1 {
					t.Fatal("empty tool list was not cached")
				}
			},
		)
	}
}
