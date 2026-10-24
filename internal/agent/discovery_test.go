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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/acp-sdk/acp"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/plugins"
)

func TestSessionDiscovery(t *testing.T) {
	const schema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	const manifest = `{"$schema":"` + schema + `","name":"example","description":"Example plugin summary.\nAdditional metadata."}`
	const instructions = "Fixture server summary.\nDetailed server instructions."
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
		connectError    bool
		reopen          bool
		multipleServers bool
		clientServer    bool
		customPrompt    bool
		setupTimeout    bool
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
			name:            "all installed MCP servers connect without executing tools",
			manifest:        manifest,
			mcp:             true,
			wantConnections: 2,
		},
		{
			name:            "direct builtins and restored sessions",
			manifest:        manifest,
			mcp:             true,
			code:            "print(mcp__example_tools__echo({'value': 'hello'}).content[0].text)",
			wantOutput:      "ready",
			wantConnections: 2,
			wantCalls:       2,
			reopen:          true,
		},
		{
			name:            "all servers in each plugin are enabled",
			manifest:        manifest,
			mcp:             true,
			multipleServers: true,
			code:            "print(mcp__example_other__echo({}).content[0].text)",
			wantOutput:      "ready",
			wantConnections: 3,
			wantCalls:       1,
		},
		{
			name:            "namespace metadata preserves server instructions",
			manifest:        manifest,
			mcp:             true,
			code:            "print(describe_namespace('mcp__example_tools')['instructions'])",
			wantOutput:      instructions,
			wantConnections: 2,
		},
		{
			name:            "tool description exposes argument schema",
			manifest:        manifest,
			mcp:             true,
			code:            "print(describe_tool('mcp__example_tools__echo')['input_schema']['properties']['value']['description'])",
			wantOutput:      "Text to echo",
			wantConnections: 2,
		},
		{
			name:            "search discovers a callable builtin without remote execution",
			manifest:        manifest,
			mcp:             true,
			code:            "print(search_tools('echo', namespace='mcp__example_tools')[0]['name'])",
			wantOutput:      "mcp__example_tools__echo",
			wantConnections: 2,
		},
		{
			name:            "namespaces augment a custom system prompt",
			manifest:        manifest,
			mcp:             true,
			customPrompt:    true,
			wantConnections: 2,
		},
		{
			name:            "client-provided servers use the same discovery and calling interface",
			manifest:        manifest,
			clientServer:    true,
			code:            "print(mcp__client__echo({}).content[0].text)",
			wantOutput:      "ready",
			wantConnections: 1,
			wantCalls:       1,
		},
		{
			name:            "MCP setup respects session deadline",
			manifest:        manifest,
			mcp:             true,
			setupTimeout:    true,
			wantError:       true,
			wantConnections: 1,
		},
		{
			name:            "failed setup closes earlier server connections",
			manifest:        manifest,
			mcp:             true,
			connectError:    true,
			wantError:       true,
			wantConnections: 2,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var connections, calls atomic.Int32
				var stderr bytes.Buffer
				var closed []<-chan struct{}
				prompts := make(
					chan string,
					1,
				)
				fixture := serverFixture(
					t,
					func(ctx context.Context, sessionID conversation.ID, model models.Model, reasoning models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
						base, options, err := scriptGenerator(
							ctx,
							sessionID,
							model,
							reasoning,
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
					func(config *Config, deps *Dependencies, initialize *acp.InitializeRequest) {
						deps.Stderr = &stderr
						if test.setupTimeout {
							config.SetupTimeout = 25 * time.Millisecond
						}
						if test.customPrompt {
							config.Instructions = "Custom base.\n\nAdditional instructions."
						}
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
							files["/work/.agents/plugins/second/plugin.json"] = `{"$schema":"` + schema + `","name":"second"}`
							files["/work/.agents/plugins/second/mcp.json"] = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"tools":{"type":"stdio","command":"second-server"}}}`
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
						deps.MCP = func(ctx context.Context, config plugins.MCPServer, toolsChanged func()) (*mcp.ClientSession, error) {
							connections.Add(1)
							if test.setupTimeout {
								<-ctx.Done()
								return nil, ctx.Err()
							}
							if test.connectError && config.Command == "second-server" {
								return nil, errors.New("fixture unavailable")
							}
							server := mcp.NewServer(
								&mcp.Implementation{
									Name:    "fixture",
									Version: "1",
								},
								&mcp.ServerOptions{
									Instructions: instructions,
								},
							)
							server.AddTool(
								&mcp.Tool{
									Name:        "echo",
									Description: "Echo a message",
									InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string","description":"Text to echo"}}}`),
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
							done := make(chan struct{})
							closed = append(
								closed,
								done,
							)
							go func() {
								_ = session.Wait()
								close(done)
							}()
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
						}
					},
				)
				request := &acp.NewSessionRequest{
					Cwd: "/work",
				}
				if test.clientServer {
					request.McpServers = []acp.McpServer{
						{
							Name:    "client",
							Command: "fixture-server",
						},
					}
				}
				response, err := fixture.client.NewSession(
					t.Context(),
					request,
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"new session: %v",
						err,
					)
				}
				if connections.Load() != test.wantConnections || calls.Load() != 0 {
					t.Fatal("session did not discover exactly its enabled servers without calling tools")
				}
				if !test.wantError {
					code := test.code
					if code == "" {
						code = "print('ready')"
					}
					promptFixture(
						t,
						fixture.client,
						response.SessionID,
						code,
					)
					system := <-prompts
					if test.mcp && (!strings.Contains(
						system,
						"- mcp__example_tools: Example plugin summary.",
					) || !strings.Contains(
						system,
						"- mcp__second_tools: Fixture server summary.",
					) || strings.Contains(
						system,
						"Detailed server instructions.",
					)) {
						t.Fatalf(
							"namespace summaries missing or not summarized: %s",
							system,
						)
					}
					if test.clientServer && !strings.Contains(
						system,
						"- mcp__client: Fixture server summary.",
					) {
						t.Fatal("client namespace missing from prompt")
					}
					if test.customPrompt && (!strings.HasPrefix(
						system,
						"Custom base.\n\nAdditional instructions.",
					) || !strings.Contains(
						system,
						"example:review",
					)) {
						t.Fatal("custom system prompt or skills were replaced")
					}
					record, err := fixture.store.Load(
						t.Context(),
						conversation.ID(response.SessionID),
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
					for _, message := range record.Dialog {
						if message.ToolResultError {
							t.Fatalf(
								"failed tool: %s",
								lastToolOutput(record),
							)
						}
					}
				}
				if test.reopen {
					promptFixture(
						t,
						fixture.client,
						response.SessionID,
						test.code,
					)
					if connections.Load() != test.wantConnections {
						t.Fatal("repeated calls opened new connections")
					}
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
					for _, operation := range []string{
						"load",
						"resume",
					} {
						if _, err := fixture.client.CloseSession(
							t.Context(),
							&acp.CloseSessionRequest{
								SessionID: response.SessionID,
							},
						); err != nil {
							t.Fatal(err)
						}
						if operation == "load" {
							_, err = fixture.client.LoadSession(
								t.Context(),
								&acp.LoadSessionRequest{
									SessionID: response.SessionID,
									Cwd:       "/work",
								},
							)
						} else {
							_, err = fixture.client.ResumeSession(
								t.Context(),
								&acp.ResumeSessionRequest{
									SessionID: response.SessionID,
									Cwd:       "/work",
								},
							)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				wantConnections := test.wantConnections
				if test.reopen {
					wantConnections *= 4
				}
				if connections.Load() != wantConnections || calls.Load() != test.wantCalls {
					t.Fatalf(
						"connections=%d calls=%d",
						connections.Load(),
						calls.Load(),
					)
				}
				if err := fixture.client.Close(); err != nil {
					t.Fatal(err)
				}
				for _, done := range closed {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Fatal("MCP connection leaked")
					}
				}
				fixture.notifications.mu.Lock()
				defer fixture.notifications.mu.Unlock()
				warned := strings.Contains(
					stderr.String(),
					`unknown manifest field "extra" ignored`,
				)
				skillFound := false
				for _, notification := range fixture.notifications.updates {
					update := notification.Update
					if update.SessionUpdate == acp.SessionUpdateTypeNotice {
						t.Fatal("discovery warning leaked into ACP updates")
					}
					if content, ok := update.Content.(acp.ContentBlock); ok && strings.Contains(
						content.Text,
						"unknown manifest field",
					) {
						t.Fatal("discovery warning leaked into ACP updates")
					}
					for _, command := range update.AvailableCommands {
						if command.Name == "example:review" {
							skillFound = true
						}
					}
				}
				if warned != test.wantWarning || !test.wantError && skillFound == test.missingRoot {
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
