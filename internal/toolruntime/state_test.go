package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/dyson"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/mcpclient"
)

func TestSessionState(t *testing.T) {
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
			setup: `state["data"] = [None, True, 1208925819614629174706176, 1.5, "text", b"bytes", ({("key", 1): [3]},)]`,
			code:  `print(state["data"] == [None, True, 1208925819614629174706176, 1.5, "text", b"bytes", ({("key", 1): [3]},)])`,
			want:  "True",
		},
		{
			name: "shared acyclic data",
			setup: `shared = [1]
state["data"] = (shared, shared)`,
			code: `print(state["data"] == ([1], [1]))`,
			want: "True",
		},
		{
			name:  "replace stored collections",
			setup: `state["data"] = [1]`,
			code: `state["data"] = list(state["data"]) + [2]
print(state["data"])`,
			want: "[1, 2]",
		},
		{
			name:  "membership iteration and length",
			setup: `state["data"] = None`,
			code:  `print("data" in state, "other" in state, list(state), len(state))`,
			want:  `True False ["data"] 1`,
		},
		{
			name: "writes survive script failure",
			setup: `state["data"] = 42
fail("later error")`,
			setupError: "later error",
			code:       `print(state["data"])`,
			want:       "42",
		},
		{
			name: "rebound state is local to invocation",
			setup: `state["data"] = 42
state = {"data": 7}`,
			code: `print(state["data"])`,
			want: "42",
		},
		{
			name:   "missing key",
			code:   `print(state["missing"])`,
			want:   "missing",
			failed: true,
		},
		{
			name:       "nonstring write key",
			setup:      `state[1] = "value"`,
			setupError: "state keys must be strings",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name:   "nonstring read key",
			code:   `print(state[1])`,
			want:   "state keys must be strings",
			failed: true,
		},
		{
			name:   "frozen nested data",
			setup:  `state["data"] = ({"items": [1]},)`,
			code:   `state["data"][0]["items"].append(print)`,
			want:   "frozen",
			failed: true,
		},
		{
			name: "aliases cannot inject runtime values",
			setup: `items = []
state["data"] = items
items.append(print)`,
			setupError: "frozen",
			code:       `print(state["data"])`,
			want:       "[]",
		},
		{
			name: "function rejected without replacing data",
			setup: `state["data"] = 42
def fn():
    return 7
state["data"] = fn`,
			setupError: "state values must be data; got function",
			code:       `print(state["data"])`,
			want:       "42",
		},
		{
			name:       "nested builtin rejected",
			setup:      `state["data"] = [{"fn": print}]`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name:       "dictionary key checked",
			setup:      `state["data"] = {print: "value"}`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name: "module rejected",
			setup: `load("json.star", "json")
state["data"] = json`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name:       "resource rejected",
			setup:      `state["data"] = open("/file")`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name:  "MCP raw data persists",
			setup: `state["data"] = mcp__test__echo({}).content[0].raw`,
			code:  `print(state["data"]["text"])`,
			want:  "test/echo",
		},
		{
			name:       "MCP result wrapper rejected",
			setup:      `state["data"] = mcp__test__echo({})`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name:       "MCP content wrapper rejected",
			setup:      `state["data"] = mcp__test__echo({}).content[0]`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name:       "state itself rejected",
			setup:      `state["data"] = state`,
			setupError: "state values must be data",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name: "list cycle rejected",
			setup: `items = []
items.append(items)
state["data"] = items`,
			setupError: "must not contain cycles",
			code:       `print(len(state))`,
			want:       "0",
		},
		{
			name: "tuple dictionary cycle rejected",
			setup: `items = {}
items["self"] = (items,)
state["data"] = items`,
			setupError: "must not contain cycles",
			code:       `print(len(state))`,
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
