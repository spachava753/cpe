package mcptools_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/gai"
	"github.com/spachava753/gai/agent/agenttest"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcptools"
	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
	"github.com/spachava753/cpe/internal/testutil/testgate"
)

const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII="

func TestConnect(t *testing.T) {
	t.Run("stdio process and replay", func(t *testing.T) {
		testgate.RequireIntegration(t)
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), binary, "-test.run=^TestMCPServerProcess$")
		command.Env = append(os.Environ(), "CPE_MCP_TEST_PROCESS=1")
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		connection, err := mcptools.Connect(ctx, "process", &mcp.CommandTransport{Command: command})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		dir := t.TempDir()
		store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		opts := repl.Options{Store: store, CWD: dir, Tools: connection.Tools()}
		r, err := repl.New(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		result, err := r.Eval(t.Context(), "process", `load("tools.star", "mcp_process__hello"); greeting = mcp_process__hello()["content"][0]["text"]; print(greeting)`)
		if err != nil || result.Error != "" || result.Output != "hello from stdio\n" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		r, err = repl.New(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		result, err = r.Eval(t.Context(), "saved", `print(greeting)`)
		if err != nil || result.Error != "" || result.Output != "hello from stdio\n" {
			t.Fatalf("restored result=%+v err=%v", result, err)
		}
	})
	t.Run("lossless structured numbers", func(t *testing.T) {
		server := mcp.NewServer(&mcp.Implementation{Name: "numbers", Version: "1"}, nil)
		var calls atomic.Int32
		server.AddTool(&mcp.Tool{Name: "echo", InputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"},"nested":{"type":"array","items":{"type":"number"}}},"required":["n","nested"]}`)}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			return &mcp.CallToolResult{Content: []mcp.Content{}, StructuredContent: req.Params.Arguments}, nil
		})
		connection := connectDummy(t, server, "HTTP JSON", "numbers")
		dir := t.TempDir()
		store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		opts := repl.Options{Store: store, CWD: dir, Tools: connection.Tools()}
		r, err := repl.New(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		result, err := r.Eval(t.Context(), "numbers", `load("tools.star", "mcp_numbers__echo")
value = mcp_numbers__echo(n=9007199254740993, nested=[123456789012345678901234567890, 0.125])["structuredContent"]
print(value["n"], value["nested"])`)
		const want = "9007199254740993 [123456789012345678901234567890, 0.125]\n"
		if err != nil || result.Error != "" || result.Output != want {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		r, err = repl.New(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		result, err = r.Eval(t.Context(), "saved numbers", `print(value["n"], value["nested"])`)
		if err != nil || result.Error != "" || result.Output != want || calls.Load() != 1 {
			t.Fatalf("restored result=%+v err=%v calls=%d", result, err, calls.Load())
		}
	})
	for _, transport := range []string{"memory", "HTTP JSON", "HTTP SSE"} {
		t.Run(transport+" orchestration and replay", func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "dummy", Version: "1"}, &mcp.ServerOptions{PageSize: 1})
			var calls atomic.Int32
			server.AddTool(&mcp.Tool{Name: "seed", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls.Add(1)
				if string(req.Params.Arguments) != "{}" {
					return nil, fmt.Errorf("no-parameter tool received %s", req.Params.Arguments)
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "40"}}}, nil
			})
			server.AddTool(&mcp.Tool{Name: "add", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"},"increment":{"type":"integer"}},"required":["value","increment"],"additionalProperties":false}`)}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls.Add(1)
				var args struct{ Value, Increment int }
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "added"}}, StructuredContent: map[string]any{"total": args.Value + args.Increment, "nested": map[string]any{"ok": true}}}, nil
			})
			server.AddTool(&mcp.Tool{Name: "describe", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"]}`)}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls.Add(1)
				var args struct{ Value int }
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("answer %d", args.Value)}}}, nil
			})
			data, err := base64.StdEncoding.DecodeString(pixel)
			if err != nil {
				t.Fatal(err)
			}
			server.AddTool(&mcp.Tool{Name: "get-media", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls.Add(1)
				return &mcp.CallToolResult{Content: []mcp.Content{
					&mcp.TextContent{Text: "a pixel"},
					&mcp.ImageContent{Data: data, MIMEType: "image/png"},
					&mcp.AudioContent{Data: []byte("audio fixture"), MIMEType: "audio/wav"},
				}, StructuredContent: map[string]any{"label": "pixel"}, Meta: mcp.Meta{"fixture": true}}, nil
			})
			connection := connectDummy(t, server, transport, "fixture")
			tools := connection.Tools()
			if len(tools) != 4 {
				t.Fatalf("paginated discovery returned %d tools", len(tools))
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "s.jsonl")
			store, err := session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			code := `load("tools.star", "mcp_fixture__seed", "mcp_fixture__add", "mcp_fixture__describe", "mcp_fixture__get_media")
load("repl.star", "emit_image")
seed = mcp_fixture__seed()
total = int(seed["content"][0]["text"])
for step in [1, 1]:
    added = mcp_fixture__add(value=total, increment=step)
    total = added["structuredContent"]["total"]
text = mcp_fixture__describe({"value": total})
saved = mcp_fixture__get_media()
print(text["content"][0]["text"], added["structuredContent"]["nested"]["ok"], saved["structuredContent"]["label"], saved["content"][2]["type"], saved["_meta"]["fixture"])
emit_image(saved["content"][1])`
			call, err := gai.ToolCallBlock("first", "starlark_repl", map[string]any{"code": code})
			if err != nil {
				t.Fatal(err)
			}
			gen := agenttest.NewScriptedGenerator(
				agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					if len(req.Tools) != 1 || req.Tools[0].Name != "starlark_repl" {
						return errors.New("MCP tools exposed directly to model")
					}
					if !strings.Contains(req.Instructions.Blocks[0].Content.String(), "mcp_fixture__get_media") {
						return errors.New("missing MCP tool instructions")
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{call}}}, FinishReason: gai.ToolUse}},
				agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					message := req.Dialog[len(req.Dialog)-1]
					if message.Role != gai.ToolResult || message.ToolResultError || len(message.Blocks) != 2 {
						return fmt.Errorf("unexpected result %+v", message)
					}
					if message.Blocks[0].Content.String() != "answer 42 True pixel audio True\n" || message.Blocks[0].ID != "first" {
						return fmt.Errorf("unexpected text %+v", message.Blocks[0])
					}
					image := message.Blocks[1]
					if image.ID != "first" || image.BlockType != gai.Content || image.ModalityType != gai.Image || image.MimeType != "image/png" || image.Content.String() != pixel {
						return fmt.Errorf("image block not preserved: %+v", image)
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("saw image")}}}, FinishReason: gai.EndTurn}},
			)
			opts := agent.Options{Config: config.Config{Agent: config.Agent{ToolTimeout: "3s", MaxRounds: 5, OutputLimit: 32000}}, Model: config.Model{ID: "fixture"}, Generator: gen, Store: store, CWD: dir, Tools: tools}
			a, err := agent.Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Prompt(t.Context(), "orchestrate", nil); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 5 {
				t.Fatalf("calls=%d, want 5", calls.Load())
			}
			checkpoints := a.Checkpoints()
			checkpoint := checkpoints[len(checkpoints)-1].ID
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}

			// The very same callbacks now point to a closed MCP session. Reopening,
			// branching, and emitting a saved image must never invoke them.
			store, err = session.Open(path, dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			opts.Store = store
			again, err := gai.ToolCallBlock("again", "starlark_repl", map[string]any{"code": `print(total); emit_image(saved["content"][1])`})
			if err != nil {
				t.Fatal(err)
			}
			opts.Generator = agenttest.NewScriptedGenerator(
				agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					message := req.Dialog[2]
					if message.Role != gai.ToolResult || message.ToolResultError || len(message.Blocks) != 2 {
						return fmt.Errorf("unexpected result %+v", message)
					}
					if message.Blocks[0].Content.String() != "answer 42 True pixel audio True\n" || message.Blocks[0].ID != "first" {
						return fmt.Errorf("unexpected text %+v", message.Blocks[0])
					}
					image := message.Blocks[1]
					if image.ID != "first" || image.BlockType != gai.Content || image.ModalityType != gai.Image || image.MimeType != "image/png" || image.Content.String() != pixel {
						return fmt.Errorf("image block not preserved: %+v", image)
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{again}}}, FinishReason: gai.ToolUse}},
				agenttest.GenerateStep{Check: func(req gai.GenerationRequest) error {
					message := req.Dialog[len(req.Dialog)-1]
					if message.Role != gai.ToolResult || message.ToolResultError || len(message.Blocks) != 2 {
						return fmt.Errorf("unexpected result %+v", message)
					}
					if message.Blocks[0].Content.String() != "42\n" || message.Blocks[0].ID != "again" {
						return fmt.Errorf("unexpected text %+v", message.Blocks[0])
					}
					image := message.Blocks[1]
					if image.ID != "again" || image.BlockType != gai.Content || image.ModalityType != gai.Image || image.MimeType != "image/png" || image.Content.String() != pixel {
						return fmt.Errorf("image block not preserved: %+v", image)
					}
					return nil
				}, Response: gai.Response{Candidates: []gai.Message{{Role: gai.Assistant, Blocks: []gai.Block{gai.TextBlock("still visible")}}}, FinishReason: gai.EndTurn}},
			)
			a, err = agent.Open(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err := a.Branch(t.Context(), checkpoint); err != nil {
				t.Fatal(err)
			}
			if err := a.Prompt(t.Context(), "show saved image", nil); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 5 {
				t.Fatalf("replayed effects: calls=%d", calls.Load())
			}
		})
	}
	t.Run("schema and tool errors", func(t *testing.T) {
		server := mcp.NewServer(&mcp.Implementation{Name: "errors", Version: "1"}, nil)
		var calls atomic.Int32
		server.AddTool(&mcp.Tool{Name: "fail", InputSchema: json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string"}},"required":["mode"],"additionalProperties":false}`)}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			var args struct{ Mode string }
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
			if args.Mode == "protocol" {
				return nil, errors.New("protocol failure")
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "tool refused"}}}, nil
		})
		c := connectDummy(t, server, "memory", "errors")
		dir := t.TempDir()
		store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		r, err := repl.New(t.Context(), repl.Options{Store: store, CWD: dir, Tools: c.Tools()})
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		for _, tc := range []struct {
			name, code, output string
			failure            bool
			calls              int32
		}{
			{"missing parameter", `load("tools.star", "mcp_errors__fail"); mcp_errors__fail()`, "", true, 0},
			{"wrong parameter type", `load("tools.star", "mcp_errors__fail"); mcp_errors__fail(mode=3)`, "", true, 0},
			{"inspectable tool error", `load("tools.star", "mcp_errors__fail"); result=mcp_errors__fail(mode="tool"); print(result["isError"], result["content"][0]["text"])`, "True tool refused\n", false, 1},
			{"protocol error", `mcp_errors__fail(mode="protocol")`, "", true, 2},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result, err := r.Eval(t.Context(), tc.name, tc.code)
				if err != nil || (result.Error != "") != tc.failure || result.Output != tc.output || calls.Load() != tc.calls {
					t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
				}
			})
		}
	})
	for _, tc := range []struct {
		name, namespace string
		tools           []string
	}{
		{"invalid namespace", "bad-name", []string{"ok"}},
		{"colliding normalized names", "ok", []string{"a-b", "a_b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "names", Version: "1"}, nil)
			for _, name := range tc.tools {
				server.AddTool(&mcp.Tool{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					t.Error("unexpected call")
					return nil, errors.New("unexpected call")
				})
			}
			left, right := mcp.NewInMemoryTransports()
			ss, err := server.Connect(t.Context(), left, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			c, err := mcptools.Connect(ctx, tc.namespace, right)
			if err == nil {
				_ = c.Close()
				t.Fatal("invalid names accepted")
			}
		})
	}
}

// This is the stdio fixture's subprocess entry point, not a live service.
func TestMCPServerProcess(t *testing.T) {
	testgate.RequireIntegration(t)
	if os.Getenv("CPE_MCP_TEST_PROCESS") != "1" {
		t.Skip("subprocess fixture only")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "stdio-fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "hello", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hello from stdio"}}}, nil
	})
	if err := server.Run(t.Context(), &mcp.StdioTransport{}); err != nil {
		t.Fatal(err)
	}
}

// connectDummy hides transport plumbing; scenarios own their tool definitions
// and behavior assertions. HTTP cases exercise real local JSON-RPC requests.
func connectDummy(t *testing.T, server *mcp.Server, kind, name string) *mcptools.Connection {
	t.Helper()
	var transport mcp.Transport
	if kind == "memory" {
		left, right := mcp.NewInMemoryTransports()
		ss, err := server.Connect(t.Context(), left, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ss.Close() })
		transport = right
	} else {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: kind == "HTTP JSON"})
		httpServer := httptest.NewServer(handler)
		t.Cleanup(httpServer.Close)
		transport = &mcp.StreamableClientTransport{Endpoint: httpServer.URL, MaxRetries: -1, DisableStandaloneSSE: true}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := mcptools.Connect(ctx, name, transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
