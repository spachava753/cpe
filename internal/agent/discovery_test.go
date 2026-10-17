package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spf13/afero"
)

func TestSessionDiscovery(t *testing.T) {
	const schema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	for _, test := range []struct {
		name            string
		manifest        string
		mcp             bool
		notices         bool
		wantWarning     bool
		wantError       bool
		missingRoot     bool
		missingManifest bool
		code            string
		wantOutput      string
		wantConnections int32
		wantCalls       int32
		wantToolError   bool
		connectError    bool
		reload          bool
		multipleServers bool
	}{
		{
			name:        "absent discovery root is optional",
			missingRoot: true,
		},
		{
			name:            "missing manifest alongside valid sibling is fatal",
			missingManifest: true,
			wantError:       true,
		},
		{
			name:        "unknown fields are warnings",
			manifest:    `{"$schema":"` + schema + `","name":"example","extra":true}`,
			notices:     true,
			wantWarning: true,
		},
		{
			name:        "stderr warnings without notices",
			manifest:    `{"$schema":"` + schema + `","name":"example","extra":true}`,
			wantWarning: true,
		},
		{
			name:      "invalid schema is fatal",
			manifest:  `{"$schema":"https://example.invalid/schema","name":"example"}`,
			wantError: true,
		},
		{
			name:      "malformed manifest is fatal",
			manifest:  `{broken`,
			wantError: true,
		},
		{
			name:     "unused plugins never connect",
			manifest: `{"$schema":"` + schema + `","name":"example"}`,
			mcp:      true,
		},
		{
			name:            "explicit plugin load and repeated loads reuse connection",
			manifest:        `{"$schema":"` + schema + `","name":"example"}`,
			mcp:             true,
			code:            "plugin = load_plugin('example')\nemit(plugin['example:tools']['echo']({}).content[0])",
			wantOutput:      "ready",
			wantConnections: 1,
			wantCalls:       1,
			reload:          true,
		},
		{
			name:            "plugin load connects all of its servers only",
			manifest:        `{"$schema":"` + schema + `","name":"example"}`,
			mcp:             true,
			multipleServers: true,
			code:            "print(sorted(load_plugin('example').keys()))",
			wantOutput:      `["example:other", "example:tools"]`,
			wantConnections: 2,
		},
		{
			name:       "plugin tools are not client-supplied tools",
			manifest:   `{"$schema":"` + schema + `","name":"example"}`,
			mcp:        true,
			code:       "print(mcp_tools)",
			wantOutput: "{}",
		},
		{
			name:          "schema inspection does not implicitly load plugin",
			manifest:      `{"$schema":"` + schema + `","name":"example"}`,
			mcp:           true,
			code:          "describe_mcp_tools('example:tools')",
			wantOutput:    "load the plugin",
			wantToolError: true,
		},
		{
			name:          "unknown plugin does not connect",
			manifest:      `{"$schema":"` + schema + `","name":"example"}`,
			mcp:           true,
			code:          "load_plugin('missing')",
			wantOutput:    "unknown plugin",
			wantToolError: true,
		},
		{
			name:            "unavailable plugin fails only when loaded",
			manifest:        `{"$schema":"` + schema + `","name":"example"}`,
			mcp:             true,
			code:            "load_plugin('example')",
			wantOutput:      "fixture unavailable",
			wantConnections: 1,
			connectError:    true,
			wantToolError:   true,
		},
		{
			name:       "skill-only plugin load has no connections",
			manifest:   `{"$schema":"` + schema + `","name":"example"}`,
			code:       "print(load_plugin('example'))",
			wantOutput: "{}",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var connections, calls atomic.Int32
				var stderr bytes.Buffer
				fixture := serverFixture(
					t,
					scriptGenerator,
					func(_ *Config, deps *Dependencies, initialize *acp.InitializeRequest) {
						deps.Stderr = &stderr
						if test.notices {
							initialize.ClientCapabilities = &acp.ClientCapabilities{
								Session: &acp.ClientSessionCapabilities{
									Notices: &acp.NoticeCapabilities{},
								},
							}
						}
						files := map[string]string{
							"/work/.agents/plugins/example/plugin.json":            test.manifest,
							"/work/.agents/plugins/example/skills/review/SKILL.md": "---\nname: review\ndescription: Review code\n---\nread only when selected",
						}
						if test.missingRoot {
							files = nil
						}
						if test.missingManifest {
							delete(
								files,
								"/work/.agents/plugins/example/plugin.json",
							)
							files["/work/.agents/plugins/valid/plugin.json"] = `{"$schema":"` + schema + `","name":"valid"}`
						}
						if test.mcp {
							files["/work/.agents/plugins/example/mcp.json"] = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"tools":{"type":"stdio","command":"fixture-server"}}}`
							if test.multipleServers {
								files["/work/.agents/plugins/example/mcp.json"] = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"other":{"type":"stdio","command":"fixture-server"},"tools":{"type":"stdio","command":"fixture-server"}}}`
							}
							files["/work/.agents/plugins/unused/plugin.json"] = `{"$schema":"` + schema + `","name":"unused"}`
							files["/work/.agents/plugins/unused/mcp.json"] = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"tools":{"type":"stdio","command":"unused-server"}}}`
						}
						for name, content := range files {
							if err := deps.Filesystem.MkdirAll(
								filepath.Dir(name),
								0o700,
							); err != nil {
								t.Fatal(err)
							}
							if err := afero.WriteFile(
								deps.Filesystem,
								name,
								[]byte(content),
								0o600,
							); err != nil {
								t.Fatal(err)
							}
						}
						deps.MCP = func(ctx context.Context, config mcpServer) (*mcp.ClientSession, error) {
							connections.Add(1)
							if config.Command == "unused-server" {
								return nil, errors.New("unused plugin must not connect")
							}
							if test.connectError {
								return nil, errors.New("fixture unavailable")
							}
							server := mcp.NewServer(
								&mcp.Implementation{
									Name:    "fixture",
									Version: "1",
								},
								nil,
							)
							server.AddTool(
								&mcp.Tool{
									Name:        "echo",
									InputSchema: json.RawMessage(`{"type":"object"}`),
								},
								func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
									calls.Add(1)
									return &mcp.CallToolResult{
										Content: []mcp.Content{
											&mcp.TextContent{
												Text: "ready",
											},
										},
									}, nil
								},
							)
							a, b := mcp.NewInMemoryTransports()
							session, err := server.Connect(
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
									Name:    "fixture",
									Version: "1",
								},
								nil,
							).Connect(
								ctx,
								b,
								nil,
							)
						}
					},
				)
				response, err := fixture.client.NewSession(
					t.Context(),
					&acp.NewSessionRequest{
						Cwd: "/work",
					},
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"new session: %v",
						err,
					)
				}
				if test.wantError {
					return
				}
				if connections.Load() != 0 || calls.Load() != 0 {
					t.Fatal("session startup connected an installed plugin")
				}
				if test.mcp {
					promptFixture(
						t,
						fixture.client,
						response.SessionID,
						"print(mcp_servers())",
					)
					if connections.Load() != 0 {
						t.Fatal("listing servers connected an installed plugin")
					}
				}
				if test.code != "" {
					promptFixture(
						t,
						fixture.client,
						response.SessionID,
						test.code,
					)
					record, err := fixture.store.Load(
						t.Context(),
						SessionID(response.SessionID),
					)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(
						lastToolOutput(record),
						test.wantOutput,
					) {
						t.Fatalf(
							"tool output: %q",
							lastToolOutput(record),
						)
					}
					failed := false
					for _, message := range record.Dialog {
						failed = failed || message.ToolResultError
					}
					if failed != test.wantToolError {
						t.Fatalf(
							"failed tool: %v",
							failed,
						)
					}
				}
				if test.reload {
					promptFixture(
						t,
						fixture.client,
						response.SessionID,
						"load_plugin('example')\nprint(describe_mcp_tools('example:tools'))",
					)
				}
				if test.reload {
					// Saved history must not reactivate plugin resources on fork/load/resume.
					fork, err := fixture.client.ForkSession(
						t.Context(),
						&acp.ForkSessionRequest{
							SessionID: response.SessionID,
							Cwd:       "/work",
						},
					)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: fork.SessionID,
						},
					); err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: response.SessionID,
						},
					); err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.client.LoadSession(
						t.Context(),
						&acp.LoadSessionRequest{
							SessionID: response.SessionID,
							Cwd:       "/work",
						},
					); err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.client.CloseSession(
						t.Context(),
						&acp.CloseSessionRequest{
							SessionID: response.SessionID,
						},
					); err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.client.ResumeSession(
						t.Context(),
						&acp.ResumeSessionRequest{
							SessionID: response.SessionID,
							Cwd:       "/work",
						},
					); err != nil {
						t.Fatal(err)
					}
				}
				if connections.Load() != test.wantConnections || calls.Load() != test.wantCalls {
					t.Fatalf(
						"connections=%d calls=%d",
						connections.Load(),
						calls.Load(),
					)
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				fixture.notifications.mu.Lock()
				defer fixture.notifications.mu.Unlock()
				warned := strings.Contains(
					stderr.String(),
					`unknown manifest field "extra" ignored`,
				)
				acpWarning, skillFound := false, false
				for _, notification := range fixture.notifications.updates {
					update := notification.Update
					if update.SessionUpdate == acp.SessionUpdateTypeNotice {
						acpWarning = true
					}
					if content, ok := update.Content.(acp.ContentBlock); ok && strings.Contains(
						content.Text,
						"unknown manifest field",
					) {
						acpWarning = true
					}
					for _, command := range update.AvailableCommands {
						if command.Name == "example:review" {
							skillFound = true
						}
					}
				}
				if acpWarning {
					t.Fatal("discovery warning leaked into ACP updates")
				}
				if warned != test.wantWarning || skillFound == test.missingRoot {
					t.Fatalf(
						"warning=%v skill discovered=%v",
						warned,
						skillFound,
					)
				}
			},
		)
	}
}
