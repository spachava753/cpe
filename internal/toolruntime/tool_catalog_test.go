package toolruntime

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/dyson"

	"github.com/spachava753/cpe/internal/mcpclient"
)

type catalogMCP struct {
	tools        map[mcpclient.ServerID][]*mcp.Tool
	instructions map[mcpclient.ServerID]string
	calls        int
	onCall       func()
}

func (m *catalogMCP) Servers() []mcpclient.ServerID {
	var servers []mcpclient.ServerID
	for server := range m.tools {
		servers = append(
			servers,
			server,
		)
	}
	return servers
}

func (m *catalogMCP) Load(ctx context.Context, id mcpclient.ServerID) ([]*mcp.Tool, error) {
	return m.tools[id], ctx.Err()
}

func (m *catalogMCP) Instructions(id mcpclient.ServerID) string { return m.instructions[id] }

func (m *catalogMCP) Call(ctx context.Context, id mcpclient.ServerID, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.calls++
	if m.onCall != nil {
		m.onCall()
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(id) + "/" + params.Name,
			},
		},
	}, nil
}

func TestToolDiscovery(t *testing.T) {
	for _, test := range []struct {
		name      string
		code      string
		want      string
		wantError bool
	}{
		{
			name: "search names and descriptions",
			code: "print(search_tools('find open pull requests')[0]['name'])",
			want: "mcp__records__listPullRequests",
		},
		{
			name: "argument property name",
			code: "print(search_tools('milestone')[0]['name'])",
			want: "mcp__records__listPullRequests",
		},
		{
			name: "nested argument description with plural normalization",
			code: "print(search_tools('enterprises')[0]['name'])",
			want: "mcp__records__listPullRequests",
		},
		{
			name: "property in referenced dollar-defs schema",
			code: "print(search_tools('sprint')[0]['name'])",
			want: "mcp__records__listPullRequests",
		},
		{
			name: "property in referenced definitions schema",
			code: "print(search_tools('board')[0]['name'])",
			want: "mcp__records__listPullRequests",
		},
		{
			name: "namespace description",
			code: "print(len(search_tools('archive')))",
			want: "2",
		},
		{
			name: "namespace server instructions",
			code: "print(len(search_tools('triage')))",
			want: "2",
		},
		{
			name: "namespace filter and result limit",
			code: "print(search_tools('find', limit=1, namespace='mcp__calendar')[0]['name'])",
			want: "mcp__calendar__listEvents",
		},
		{
			name: "empty matches",
			code: "print(search_tools('unrelated'))",
			want: "[]",
		},
		{
			name: "meaningless query",
			code: "print(search_tools('the and a'))",
			want: "[]",
		},
		{
			name:      "invalid result limit",
			code:      "search_tools('find', limit=0)",
			want:      "search limit must be positive",
			wantError: true,
		},
		{
			name: "tool input schema and signature",
			code: "tool = describe_tool('mcp__records__listPullRequests')\nprint(tool['signature'])\nprint(tool['input_schema']['properties']['milestone']['type'])",
			want: "mcp__records__listPullRequests(arguments = {})\nstring",
		},
		{
			name: "full namespace metadata",
			code: "ns = describe_namespace('mcp__records')\nprint(ns['description'])\nprint(ns['instructions'])\nprint(len(ns['tools']))",
			want: "Archive service\nIssue triage.\nDetailed instructions.\n2",
		},
		{
			name: "unknown metadata lookup",
			code: "print(describe_tool('unknown'))\nprint(describe_namespace('unknown'))",
			want: "None\nNone",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				remote := &catalogMCP{
					tools: map[mcpclient.ServerID][]*mcp.Tool{
						"records": {
							{
								Name:        "listPullRequests",
								Description: "Find open pull requests for a repository",
								InputSchema: json.RawMessage(`{"type":"object","properties":{"milestone":{"type":"string"},"filters":{"type":"array","items":{"anyOf":[{"type":"object","properties":{"owner":{"type":"string","description":"Enterprise team handle"}}}]}},"filter":{"$ref":"#/$defs/Filter"},"selector":{"$ref":"#/definitions/Selector"}},"$defs":{"Filter":{"type":"object","properties":{"sprint":{"type":"string"}}}},"definitions":{"Selector":{"type":"object","properties":{"board":{"type":"string"}}}}}`),
							},
							{
								Name:        "createIssue",
								Description: "Create an issue",
								InputSchema: json.RawMessage(`{"type":"object"}`),
							},
						},
						"calendar": {
							{
								Name:        "listEvents",
								Description: "Find calendar events",
								InputSchema: json.RawMessage(`{"type":"object"}`),
							},
						},
					},
					instructions: map[mcpclient.ServerID]string{
						"records": "Issue triage.\nDetailed instructions.",
					},
				}
				host := hostFixture(
					t,
					remote,
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
						return dyson.CommandResult{}, nil
					}),
					func(config *Config) {
						config.Descriptions = map[mcpclient.ServerID]string{
							"records": "Archive service",
						}
					},
				)
				result, err := host.execute(
					t.Context(),
					toolRequest(
						"starlark",
						"code",
						test.code,
					),
				)
				if err != nil || result.ToolResultError != test.wantError {
					t.Fatalf(
						"execute: %#v %v",
						result,
						err,
					)
				}
				var output strings.Builder
				for _, block := range result.Blocks {
					output.WriteString(block.Content.String())
				}
				if !strings.Contains(
					output.String(),
					test.want,
				) || remote.calls != 0 {
					t.Fatalf(
						"discovery output=%q remote calls=%d",
						output.String(),
						remote.calls,
					)
				}
			},
		)
	}
}

func TestMCPIdentifiers(t *testing.T) {
	for _, test := range []struct {
		name    string
		servers []mcpclient.ServerID
		tools   []string
	}{
		{
			name: "punctuation and keyword tool names",
			servers: []mcpclient.ServerID{
				"plugin:server",
			},
			tools: []string{
				"find.item",
				"for",
			},
		},
		{
			name: "server normalization collisions",
			servers: []mcpclient.ServerID{
				"my-server",
				"my_server",
			},
			tools: []string{
				"find",
			},
		},
		{
			name: "tool normalization collisions",
			servers: []mcpclient.ServerID{
				"test",
			},
			tools: []string{
				"find-item",
				"find_item",
			},
		},
		{
			name: "namespace boundary collisions",
			servers: []mcpclient.ServerID{
				"a",
				"a__b",
			},
			tools: []string{
				"b__c",
				"c",
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				remote := &catalogMCP{
					tools: make(map[mcpclient.ServerID][]*mcp.Tool),
				}
				for _, server := range test.servers {
					for _, name := range test.tools {
						remote.tools[server] = append(
							remote.tools[server],
							&mcp.Tool{
								Name:        name,
								InputSchema: json.RawMessage(`{"type":"object"}`),
							},
						)
					}
				}
				host := hostFixture(
					t,
					remote,
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
						return dyson.CommandResult{}, nil
					}),
				)
				seen := make(map[string]bool)
				for _, tool := range host.catalog.tools {
					if seen[tool.Name] {
						t.Fatal("MCP builtins shadow one another")
					}
					seen[tool.Name] = true
					result, err := host.execute(
						t.Context(),
						toolRequest(
							"starlark",
							"code",
							"print("+tool.Name+"({}).content[0].text)",
						),
					)
					if err != nil || result.ToolResultError || !strings.Contains(
						result.Blocks[0].Content.String(),
						string(tool.server)+"/"+tool.remoteName,
					) {
						t.Fatalf(
							"misrouted builtin %s: %#v %v",
							tool.Name,
							result,
							err,
						)
					}
				}
				if remote.calls != len(test.servers)*len(test.tools) {
					t.Fatal("tools were lost during registration")
				}
				for _, server := range test.servers {
					slices.Reverse(remote.tools[server])
				}
				again, err := discoverTools(
					t.Context(),
					remote,
					nil,
				)
				if err != nil || !reflect.DeepEqual(
					host.catalog,
					again,
				) {
					t.Fatalf(
						"identifiers depend on discovery order: %v",
						err,
					)
				}
			},
		)
	}
}

func TestToolSearchRanking(t *testing.T) {
	catalog := toolCatalog{
		tools: []catalogTool{
			{
				Name:       "generic",
				Namespace:  "records",
				searchText: "repository status commits history branches tags users projects changes",
			},
			{
				Name:       "specific",
				Namespace:  "records",
				searchText: "repository pull request",
			},
		},
	}
	for _, test := range []struct {
		name      string
		query     string
		limit     int
		namespace string
		want      []string
	}{
		{
			name:  "more matching terms rank first",
			query: "repository pull requests",
			limit: 8,
			want: []string{
				"specific",
				"generic",
			},
		},
		{
			name:  "short focused description ranks above broad description",
			query: "repository",
			limit: 1,
			want: []string{
				"specific",
			},
		},
		{
			name:      "unrelated namespace has no matches",
			query:     "repository",
			limit:     8,
			namespace: "calendar",
			want:      []string{},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				matches := catalog.search(
					test.query,
					test.limit,
					test.namespace,
				)
				names := []string{}
				for _, match := range matches {
					names = append(
						names,
						match.Name,
					)
				}
				if !slices.Equal(
					names,
					test.want,
				) {
					t.Fatalf(
						"ranked matches: %v",
						names,
					)
				}
			},
		)
	}
}

func TestToolCatalogRefresh(t *testing.T) {
	original := []*mcp.Tool{
		{
			Name:        "echo",
			Description: "old metadata",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
		{
			Name:        "obsolete",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
	}
	updated := []*mcp.Tool{
		{
			Name:        "echo",
			Description: "updated metadata",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		},
		{
			Name:        "added",
			Description: "newly available",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
	}
	remote := &catalogMCP{
		tools: map[mcpclient.ServerID][]*mcp.Tool{
			"test": original,
		},
	}
	remote.onCall = func() { remote.tools["test"] = updated }
	host := hostFixture(
		t,
		remote,
		runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
			return dyson.CommandResult{}, nil
		}),
	)
	for _, test := range []struct {
		name   string
		tools  []*mcp.Tool
		code   string
		want   string
		failed bool
	}{
		{
			name: "change during call keeps discovery snapshot",
			code: `mcp__test__echo({})
print(describe_tool("mcp__test__echo")["description"])
print(describe_tool("mcp__test__added"))
print(search_tools("newly"))
print(describe_namespace("mcp__test")["tools"])`,
			want: "old metadata\nNone\n[]\n[\"mcp__test__echo\", \"mcp__test__obsolete\"]\n",
		},
		{
			name: "next call sees metadata schemas search and new builtin",
			code: `print(describe_tool("mcp__test__echo")["description"])
print(describe_tool("mcp__test__echo")["input_schema"]["properties"]["query"]["type"])
print(search_tools("newly")[0]["name"])
print(describe_tool("mcp__test__obsolete"))
print(describe_namespace("mcp__test")["tools"])
print(mcp__test__added({}).content[0].text)`,
			want: "updated metadata\nstring\nmcp__test__added\nNone\n[\"mcp__test__added\", \"mcp__test__echo\"]\ntest/added",
		},
		{
			name:   "removed builtin is unavailable",
			code:   `mcp__test__obsolete({})`,
			want:   "undefined: mcp__test__obsolete",
			failed: true,
		},
		{
			name: "invalid refreshed catalog prevents execution",
			tools: []*mcp.Tool{
				{
					Name:        "broken",
					InputSchema: json.RawMessage(`null`),
				},
			},
			code:   `state["executed"] = True`,
			want:   "requires an input schema object",
			failed: true,
		},
		{
			name:  "refresh recovers without replaying failed script",
			tools: updated,
			code: `print("executed" in state)
print(mcp__test__added({}).content[0].text)`,
			want: "False\ntest/added",
		},
		{
			name:  "empty catalog removes all tools",
			tools: []*mcp.Tool{},
			code: `print(describe_namespace("mcp__test")["tools"])
print(search_tools("echo"))`,
			want: "[]\n[]\n",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.tools != nil {
					remote.tools["test"] = test.tools
				}
				result, err := host.execute(
					t.Context(),
					toolRequest(
						starlarkToolName,
						"code",
						test.code,
					),
				)
				var output strings.Builder
				for _, block := range result.Blocks {
					output.WriteString(block.Content.String())
				}
				if err != nil || result.ToolResultError != test.failed || !strings.Contains(
					output.String(),
					test.want,
				) {
					t.Fatalf(
						"catalog execution: %v, %q, %v; want %q",
						result.ToolResultError,
						output.String(),
						err,
						test.want,
					)
				}
			},
		)
	}
}
