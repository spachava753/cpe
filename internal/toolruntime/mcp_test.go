package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTool(server *mcp.Server, tool *mcp.Tool, handler func(context.Context, map[string]any) *mcp.CallToolResult) {
	if tool.InputSchema == nil {
		tool.InputSchema = json.RawMessage(`{"type":"object"}`)
	}
	server.AddTool(
		tool,
		func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var input map[string]any
			if err := json.Unmarshal(
				request.Params.Arguments,
				&input,
			); err != nil {
				return nil, err
			}
			return handler(
				ctx,
				input,
			), nil
		},
	)
}

func text(value string) []mcp.Content {
	return []mcp.Content{
		&mcp.TextContent{
			Text: value,
		},
	}
}

func fixtureServers() map[string]*mcp.Server {
	github := mcp.NewServer(
		&mcp.Implementation{
			Name:        "github",
			Version:     "1",
			Description: "GitHub's official MCP Server.",
		},
		&mcp.ServerOptions{
			Instructions: "Use the repo qualifier for code search.",
		},
	)
	destructive := false
	addTool(
		github,
		&mcp.Tool{
			Name:        "create-issue",
			Title:       "Create issue",
			Description: "Create a new issue in a GitHub repository.\nMore detail here.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "owner": {"type": "string", "description": "Repository owner"},
    "labels": {"type": "array", "items": {"type": "object", "properties": {"milestone": {"type": "string", "description": "Release train"}}}}
  },
  "required": ["owner"]
}`),
			Annotations: &mcp.ToolAnnotations{
				DestructiveHint: &destructive,
				IdempotentHint:  false,
			},
		},
		func(_ context.Context, input map[string]any) *mcp.CallToolResult {
			encoded, _ := json.Marshal(input)
			return &mcp.CallToolResult{
				Content: text("created " + string(encoded)),
				StructuredContent: map[string]any{
					"number": 7,
				},
			}
		},
	)
	addTool(
		github,
		&mcp.Tool{
			Name:        "fail",
			Description: "Always fails.",
		},
		func(context.Context, map[string]any) *mcp.CallToolResult {
			return &mcp.CallToolResult{
				Content: text("owner is required"),
				IsError: true,
			}
		},
	)
	addTool(
		github,
		&mcp.Tool{
			Name:        "media",
			Description: "Returns every content kind.",
		},
		func(context.Context, map[string]any) *mcp.CallToolResult {
			size := int64(12)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: "first",
					},
					&mcp.ImageContent{
						Data:     []byte("\x89PNG\r\n\x1a\n"),
						MIMEType: "image/png",
					},
					&mcp.AudioContent{
						Data:     []byte("RIFF"),
						MIMEType: "audio/wav",
					},
					&mcp.ResourceLink{
						URI:      "file:///report.pdf",
						Name:     "report",
						MIMEType: "application/pdf",
						Size:     &size,
					},
					&mcp.EmbeddedResource{
						Resource: &mcp.ResourceContents{
							URI:      "file:///blob",
							MIMEType: "application/octet-stream",
							Blob:     []byte{1, 2, 3},
						},
					},
					&mcp.TextContent{
						Text: "second",
					},
				},
			}
		},
	)
	addTool(
		github,
		&mcp.Tool{
			Name:        "slow",
			Description: "Sleeps, then echoes its input.",
		},
		func(ctx context.Context, input map[string]any) *mcp.CallToolResult {
			select {
			case <-time.After(400 * time.Millisecond):
			case <-ctx.Done():
			}
			return &mcp.CallToolResult{
				Content: text(input["value"].(string)),
			}
		},
	)
	linear := mcp.NewServer(
		&mcp.Implementation{
			Name:        "linear",
			Version:     "1",
			Description: "Team issue tracker.",
		},
		nil,
	)
	addTool(
		linear,
		&mcp.Tool{
			Name:        "list_projects",
			Description: "List projects.",
		},
		func(context.Context, map[string]any) *mcp.CallToolResult {
			return &mcp.CallToolResult{
				Content: text("projects"),
			}
		},
	)
	return map[string]*mcp.Server{
		"github": github,
		"linear": linear,
	}
}

func TestMCPToolCalls(t *testing.T) {
	r := newTestRuntime(
		t,
		fixtureServers(),
	)
	text, failed := r.script(
		t,
		`out = tools.github__create_issue(owner = "me", labels = [])
print(out.is_error, out.text, out.structured_content["number"], type(out.meta))
out = tools.github__create_issue({"owner": "dict-form"})
print(out.text)
bad = tools.github__fail()
print(bad.is_error, bad.text)
media = tools.github__media()
print([block.type for block in media.content])
print(media.text)
print(media.content[1].data[:4], media.content[1].mime_type, media.content[2].mime_type)
print(media.content[3].uri, media.content[3].size, media.content[4].data, media.content[4].text)
view(media.content[1].data, media.content[1].mime_type)
print(dir(tools))`,
	)
	want := `False created {"labels":[],"owner":"me"} 7 dict
created {"owner":"dict-form"}
True owner is required
["text", "image", "audio", "resource_link", "resource", "text"]
first
second
b"\x89PNG" image/png audio/wav
file:///report.pdf 12 b"\x01\x02\x03" None
["github__create_issue", "github__fail", "github__media", "github__slow", "linear__list_projects"]`
	if failed || text != want {
		t.Fatalf(
			"output = %q, failed = %t",
			text,
			failed,
		)
	}
	for code, want := range map[string]string{
		`tools.github__create_issue({"owner": "x"}, owner = "y")`: "not both",
		`tools.github__create_issue("x")`:                         "must be a dictionary",
		`tools.github__create_issue({"owner": b"x"})`:             "JSON-compatible",
		`tools.missing__tool()`:                                   "has no .missing__tool attribute",
	} {
		text, failed := r.script(
			t,
			code,
		)
		if !failed || !strings.Contains(
			text,
			want,
		) {
			t.Fatalf(
				"%s: output = %q",
				code,
				text,
			)
		}
	}
}

func TestDescribeAndSearch(t *testing.T) {
	r := newTestRuntime(
		t,
		fixtureServers(),
	)
	text, failed := r.script(
		t,
		`d = describe_tool("tools.github__create_issue")
print(d.name, d.mcp_name, d.server, d.title)
print(d.description)
print(sorted(d.input_schema["properties"].keys()), d.output_schema)
print(d.annotations.read_only, d.annotations.destructive, d.annotations.idempotent, d.annotations.open_world)
print(describe_tool("github__fail").annotations.read_only, describe_tool("nope"))
s = describe_server("github")
print(s.name, s.configured_name, s.description, s.instructions, s.tools)
print(describe_server("nope"))
print(describe_server("linear").description)
print([m.name for m in search_tools("repository owner")])
print([m.name for m in search_tools("milestone")])
print([m.name for m in search_tools("tracker")])
print([m.server for m in search_tools("qualifier", limit = 1)])
print([m.name for m in search_tools("description", server = "linear")])
print(search_tools("create issue")[0].description)
print(search_tools("zzzz"))`,
	)
	want := `github__create_issue create-issue github Create issue
Create a new issue in a GitHub repository.
More detail here.
["labels", "owner"] None
False False False None
None None
github github configured github Use the repo qualifier for code search. ["github__create_issue", "github__fail", "github__media", "github__slow"]
None
configured linear
["github__create_issue"]
["github__create_issue"]
["linear__list_projects"]
["github"]
[]
Create a new issue in a GitHub repository.
[]`
	if failed || text != want {
		t.Fatalf(
			"output = %q, failed = %t",
			text,
			failed,
		)
	}
	for code, want := range map[string]string{
		`search_tools("x", server = "nope")`: "unknown server",
		`search_tools("x", limit = 0)`:       "limit must be positive",
	} {
		text, failed := r.script(
			t,
			code,
		)
		if !failed || !strings.Contains(
			text,
			want,
		) {
			t.Fatalf(
				"%s: output = %q",
				code,
				text,
			)
		}
	}
}

func TestParallel(t *testing.T) {
	r := newTestRuntime(
		t,
		fixtureServers(),
	)
	start := time.Now()
	text, failed := r.script(
		t,
		`inputs = {"value": "a"}
outs = parallel([
    (tools.github__slow, inputs),
    (tools.github__slow, {"value": "b"}),
    (tools.github__slow, {"value": "c"}),
    (bash, {"script": "echo from-bash"}),
    (read_file, {"path": "missing-is-fine-later"}) if False else (bash, {"script": "printf ok"}),
])
print([o.text for o in outs[:3]], outs[3].output, outs[4].output)
inputs["value"] = "mutable after parallel"
print(inputs)`,
	)
	elapsed := time.Since(start)
	if failed || text != "[\"a\", \"b\", \"c\"] from-bash\n ok\n{\"value\": \"mutable after parallel\"}" {
		t.Fatalf(
			"output = %q, failed = %t",
			text,
			failed,
		)
	}
	if elapsed > 1100*time.Millisecond {
		t.Fatalf(
			"three 400ms calls took %s; they did not run concurrently",
			elapsed,
		)
	}
	for code, want := range map[string]string{
		"def f():\n    pass\nparallel([(f, {})])":                     "must use an MCP tool",
		"parallel([(print, {})])":                                     "must use an MCP tool",
		"parallel([tools.github__slow])":                              "(function, input) tuple",
		"parallel([(tools.github__slow, 1)])":                         "dictionary of keyword arguments",
		"parallel([(read_file, {\"path\": \"missing\"})])":            "call 0",
		"parallel([(tools.github__slow, {\"value\": set([1])})])":     "may contain only",
		"parallel([(bash, {\"script\": \"true\"}), (bash, {})])\nx=1": "missing argument for script",
	} {
		text, failed := r.script(
			t,
			code,
		)
		if !failed || !strings.Contains(
			text,
			want,
		) {
			t.Fatalf(
				"%q: output = %q",
				code,
				text,
			)
		}
	}
}
