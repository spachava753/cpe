package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/dyson"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/mcpclient"
)

func TestSessionKV(t *testing.T) {
	for _, test := range []struct {
		name       string
		setup      string
		setupError string
		code       string
		want       string
		failed     bool
	}{
		{
			name:  "scalar and nested data",
			setup: `kv.store("data", [None, True, 1208925819614629174706176, 1.5, "text", b"bytes", ({("key", 1): [3]},)])`,
			code:  `print(kv.get("data") == [None, True, 1208925819614629174706176, 1.5, "text", b"bytes", ({("key", 1): [3]},)])`,
			want:  "True",
		},
		{
			name:  "non-UTF-8 strings bytes and special floats",
			setup: `kv.store("data", ["\u00ff"[1:], b"\x00\xff", False, float("inf"), float("-inf"), float("nan"), -0.0])`,
			code: `data = kv.get("data")
print(data[:5] == ["\u00ff"[1:], b"\x00\xff", False, float("inf"), float("-inf")], str(data[5]), str(data[6]))`,
			want: "True nan -0.0",
		},
		{
			name: "sorted independent keys",
			setup: `kv.store("z", 1)
kv.store("a", 2)`,
			code: `keys = kv.keys()
keys.append("other")
print(kv.keys())`,
			want: `["a", "z"]`,
		},
		{
			name: "shared acyclic data",
			setup: `shared = [1]
kv.store("data", (shared, shared))`,
			code: `print(kv.get("data") == ([1], [1]))`,
			want: "True",
		},
		{
			name:  "replace stored collections",
			setup: `kv.store("data", [1])`,
			code: `kv.store("data", list(kv.get("data")) + [2])
print(kv.get("data"))`,
			want: "[1, 2]",
		},
		{
			name:  "membership iteration and length",
			setup: `kv.store("data", None)`,
			code:  `print("data" in kv.keys(), "other" in kv.keys(), kv.keys(), len(kv.keys()))`,
			want:  `True False ["data"] 1`,
		},
		{
			name: "writes survive script failure",
			setup: `kv.store("data", 42)
fail("later error")`,
			setupError: "later error",
			code:       `print(kv.get("data"))`,
			want:       "42",
		},
		{
			name: "rebound kv is local to invocation",
			setup: `kv.store("data", 42)
kv = {"data": 7}`,
			code: `print(kv.get("data"))`,
			want: "42",
		},
		{
			name:   "missing key",
			code:   `print(kv.get("missing"))`,
			want:   "missing",
			failed: true,
		},
		{
			name:       "nonstring write key",
			setup:      `kv.store(1, "value")`,
			setupError: "got int, want string",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name:   "nonstring read key",
			code:   `print(kv.get(1))`,
			want:   "got int, want string",
			failed: true,
		},
		{
			name:  "loaded values are independent mutable copies",
			setup: `kv.store("data", ({"items": [1]},))`,
			code: `items = kv.get("data")
items[0]["items"].append(print)
print(kv.get("data"))`,
			want: `({"items": [1]},)`,
		},
		{
			name: "store snapshots without freezing aliases",
			setup: `items = []
kv.store("data", items)
items.append(print)`,
			code: `print(kv.get("data"))`,
			want: "[]",
		},
		{
			name: "function rejected without replacing data",
			setup: `kv.store("data", 42)
def fn():
    return 7
kv.store("data", fn)`,
			setupError: "KV values must be data; got function",
			code:       `print(kv.get("data"))`,
			want:       "42",
		},
		{
			name:       "nested builtin rejected",
			setup:      `kv.store("data", [{"fn": print}])`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name:       "dictionary key checked",
			setup:      `kv.store("data", {print: "value"})`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name: "module rejected",
			setup: `load("json.star", "json")
kv.store("data", json)`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name:       "resource rejected",
			setup:      `kv.store("data", open("/file"))`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name:  "MCP raw data persists",
			setup: `kv.store("data", mcp__test__echo({}).content[0].raw)`,
			code:  `print(kv.get("data")["text"])`,
			want:  "test/echo",
		},
		{
			name:       "MCP result wrapper rejected",
			setup:      `kv.store("data", mcp__test__echo({}))`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name:       "MCP content wrapper rejected",
			setup:      `kv.store("data", mcp__test__echo({}).content[0])`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name:       "KV module rejected",
			setup:      `kv.store("data", kv)`,
			setupError: "KV values must be data",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name: "list cycle rejected",
			setup: `items = []
items.append(items)
kv.store("data", items)`,
			setupError: "must not contain cycles",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
		{
			name: "tuple dictionary cycle rejected",
			setup: `items = {}
items["self"] = (items,)
kv.store("data", items)`,
			setupError: "must not contain cycles",
			code:       `print(len(kv.keys()))`,
			want:       "0",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				host := hostFixture(
					t,
					&catalogMCP{
						tools: map[mcpclient.ServerID][]*mcp.Tool{
							"test": {
								{
									Name:        "echo",
									InputSchema: json.RawMessage(`{"type":"object"}`),
								},
							},
						},
					},
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) { return dyson.CommandResult{}, nil }),
				)
				if err := afero.WriteFile(
					host.stdlib.FS,
					"/file",
					nil,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				if test.setup != "" {
					result, err := host.execute(
						t.Context(),
						toolRequest(
							starlarkToolName,
							"code",
							test.setup,
						),
					)
					if err != nil || result.ToolResultError != (test.setupError != "") || !strings.Contains(
						result.Blocks[0].Content.String(),
						test.setupError,
					) {
						t.Fatalf(
							"setup: %v, %v; want %q",
							result,
							err,
							test.setupError,
						)
					}
				}
				result, err := host.execute(
					t.Context(),
					toolRequest(
						starlarkToolName,
						"code",
						test.code,
					),
				)
				if err != nil || result.ToolResultError != test.failed || !strings.Contains(
					result.Blocks[0].Content.String(),
					test.want,
				) {
					t.Fatalf(
						"execution: %v, %v; want %q (failed=%v)",
						result,
						err,
						test.want,
						test.failed,
					)
				}
			},
		)
	}
}

type memoryKV map[string][]byte

func (s memoryKV) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, ok := s[key]
	if !ok {
		return nil, errors.New("missing KV key: " + key)
	}
	return bytes.Clone(value), nil
}
func (s memoryKV) Store(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s[key] = bytes.Clone(value)
	return nil
}
func (s memoryKV) Keys(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(s)), nil
}
