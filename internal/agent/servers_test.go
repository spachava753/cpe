package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/mcpclient"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/plugins"
	"github.com/spachava753/cpe/internal/skills"
)

// echoConnector serves one "lookup" tool from an in-memory server named after
// the transport command.
func echoConnector(t *testing.T) mcpclient.Connector {
	return func(ctx context.Context, server plugins.MCPServer, _ io.Writer) (*mcp.ClientSession, error) {
		remote := mcp.NewServer(
			&mcp.Implementation{
				Name:        server.Command,
				Version:     "1",
				Description: "Reported by " + server.Command + ".",
			},
			nil,
		)
		remote.AddTool(
			&mcp.Tool{
				Name:        "lookup",
				Description: "Look something up.",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: "answer from " + server.Command,
						},
					},
				}, nil
			},
		)
		a, b := mcp.NewInMemoryTransports()
		session, err := remote.Connect(
			ctx,
			a,
			nil,
		)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = session.Close() })
		return mcp.NewClient(
			&mcp.Implementation{
				Name:    "test",
				Version: "1",
			},
			nil,
		).Connect(
			ctx,
			b,
			nil,
		)
	}
}

func TestSettingsServers(t *testing.T) {
	for _, test := range []struct {
		name       string
		wantError  string
		wantPrompt string
		wantStderr string
		client     []acp.McpServer
	}{
		{
			name:       "settings servers are connected, described, and callable",
			wantPrompt: "# MCP servers\n\nCall these tools from starlark_exec as tools.<name>(...); use describe_tool(name) for a tool's input schema.\n\n## docs\nConfigured docs description.\n- docs__lookup: Look something up.\n\n## wiki\nReported by wiki-server.\n- wiki__lookup: Look something up.",
			wantStderr: `cpe: skipping MCP server "broken": invalid configuration: command or url is required`,
		},
		{
			name: "a client server with a configured name fails the session",
			client: []acp.McpServer{
				{
					Name:    "docs",
					Command: "client-server",
				},
			},
			wantError: `MCP server name "docs" is defined by both settings and ACP client`,
		},
		{
			name: "invalid settings entries still claim their names",
			client: []acp.McpServer{
				{
					Name:    "broken",
					Command: "client-server",
				},
			},
			wantError: `MCP server name "broken" is defined by both settings and ACP client`,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var stderr bytes.Buffer
				prompts := make(
					chan string,
					1,
				)
				fixture := serverFixture(
					t,
					func(ctx context.Context, id conversation.ID, model models.Model, effort models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						base, options, err := scriptGenerator(
							ctx,
							id,
							model,
							effort,
						)
						return generatorFunc(func(ctx context.Context, request gai.GenerationRequest) (gai.Response, error) {
							select {
							case prompts <- request.Instructions.Blocks[0].Content.String():
							default:
							}
							return base.Generate(
								ctx,
								request,
							)
						}), options, err
					},
					func(config *Config, deps *Dependencies, _ *acp.InitializeRequest) {
						deps.Stderr = &stderr
						deps.MCP = testPool(
							t,
							echoConnector(t),
						)
						config.MCPServers = settingsServers()
					},
				)
				response, err := fixture.client.NewSession(
					t.Context(),
					&acp.NewSessionRequest{
						Cwd:        "/work",
						McpServers: test.client,
					},
				)
				if test.wantError != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.wantError,
					) {
						t.Fatalf(
							"new session error = %v",
							err,
						)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				promptFixture(
					t,
					fixture.client,
					response.SessionID,
					"print(tools.docs__lookup().text)",
				)
				system := <-prompts
				if !strings.HasSuffix(
					strings.TrimRight(
						system,
						"\n",
					),
					test.wantPrompt,
				) {
					t.Fatalf(
						"system prompt = %q",
						system,
					)
				}
				if !strings.Contains(
					stderr.String(),
					test.wantStderr,
				) {
					t.Fatalf(
						"stderr = %q",
						stderr.String(),
					)
				}
				record, err := fixture.store.Load(
					t.Context(),
					conversation.ID(response.SessionID),
				)
				if err != nil || lastToolOutput(record) != "answer from docs-server" {
					t.Fatalf(
						"tool output = %q, %v",
						lastToolOutput(record),
						err,
					)
				}
			},
		)
	}
}

func settingsServers() map[string]config.MCPServer {
	disabled := false
	return map[string]config.MCPServer{
		"docs": {
			Command:     "docs-server",
			Description: "Configured docs description.",
		},
		"wiki": {
			Command: "wiki-server",
		},
		"off": {
			Command: "never-started",
			Enabled: &disabled,
		},
		"broken": {},
	}
}

func TestExtensionInstructions(t *testing.T) {
	catalog := commandCatalog{
		commands: map[string]skills.Skill{
			"release-notes": {
				Name:        "release-notes",
				Description: "Draft release notes.",
				Path:        "/skills/release-notes/SKILL.md",
			},
			"hidden": {
				Name:               "hidden",
				Description:        "Not for the model.",
				Path:               "/skills/hidden/SKILL.md",
				InvocationDisabled: true,
			},
			"github-workflow:triage": {
				Name:        "triage",
				Description: "Label new issues.",
				Path:        "/plugins/github-workflow/skills/triage/SKILL.md",
			},
		},
		pluginSkills: map[string]bool{
			"github-workflow:triage": true,
		},
		plugins: []plugins.Plugin{
			{
				Name:        "github-workflow",
				Description: "Triage issues on GitHub.",
			},
			{
				Name: "empty",
			},
		},
	}
	tools, err := mcpclient.NewCatalog(
		map[mcpclient.ServerID]string{
			"github-workflow:github": "github_workflow_github",
			"notes":                  "notes",
		},
		[]*mcpclient.Server{
			{
				Spec: mcpclient.ServerSpec{
					ID:     "github-workflow:github",
					Source: mcpclient.SourcePlugin,
					Plugin: "github-workflow",
				},
				Info: mcp.Implementation{
					Description: "GitHub's official MCP Server.",
				},
				Tools: []*mcp.Tool{
					{
						Name:        "create-issue",
						Description: "Create an issue.\nDetails that stay out of the prompt.",
					},
				},
			},
			{
				Spec: mcpclient.ServerSpec{
					ID:     "notes",
					Source: mcpclient.SourceClient,
				},
				Tools: []*mcp.Tool{
					{
						Name: "append",
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalog.extensionInstructions(tools)
	if err != nil {
		t.Fatal(err)
	}
	want := `

# Available skills

Read a skill's SKILL.md with bash or read_file when it is relevant to the task. Paths in a skill are relative to its SKILL.md.

- release-notes: Draft release notes.
  file: "/skills/release-notes/SKILL.md"

# Available plugins

Each plugin bundles skills and MCP servers. Read its skills like any other skill; call its tools from starlark_exec.

## empty

## github-workflow
Triage issues on GitHub.

Skills:
- github-workflow:triage: Label new issues.
  file: "/plugins/github-workflow/skills/triage/SKILL.md"

MCP server github_workflow_github: GitHub's official MCP Server.
- github_workflow_github__create_issue: Create an issue.

# MCP servers

Call these tools from starlark_exec as tools.<name>(...); use describe_tool(name) for a tool's input schema.

## notes
- notes__append
`
	if diff := cmp.Diff(
		want,
		got,
	); diff != "" {
		t.Fatalf(
			"instructions (-want +got):\n%s",
			diff,
		)
	}
}
