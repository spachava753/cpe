package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/plugins"
)

func TestCommandDiscovery(t *testing.T) {
	for _, test := range []struct {
		name   string
		prompt string
		want   string
	}{
		{
			name:   "project overrides global",
			prompt: "/review changes",
			want:   "/project/.agents/skills/review/SKILL.md",
		},
		{
			name:   "global remains available",
			prompt: "/global context",
			want:   "/global/global/SKILL.md",
		},
		{
			name:   "unknown command",
			prompt: "/missing",
			want:   "/missing",
		},
		{
			name:   "absolute path at beginning",
			prompt: "/tmp/example.go has a bug",
			want:   "/tmp/example.go has a bug",
		},
		{
			name:   "absolute path in middle",
			prompt: "Fix /tmp/example.go",
			want:   "Fix /tmp/example.go",
		},
		{
			name:   "known command followed by newline",
			prompt: "/review\ninspect this diff",
			want:   "User request:\ninspect this diff",
		},
		{
			name:   "ordinary prompt",
			prompt: "hello",
			want:   "hello",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				for path, body := range map[string]string{
					"/global/review/SKILL.md":                 "---\nname: review\ndescription: Review changes\n---\nglobal review",
					"/global/global/SKILL.md":                 "---\nname: global\ndescription: Global skill\n---\nglobal definition",
					"/project/.agents/skills/review/SKILL.md": "---\nname: review\ndescription: Review changes\n---\nproject definition",
				} {
					if err := filesystem.MkdirAll(
						filepath.Dir(path),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						filesystem,
						path,
						[]byte(body),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				catalog, err := loadCommands(
					t.Context(),
					filesystem,
					"/project",
					Config{
						SkillPaths: []string{
							"/global",
						},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				instructions := catalog.instructions()
				if !strings.Contains(
					instructions,
					"/project/.agents/skills/review/SKILL.md",
				) || strings.Contains(
					instructions,
					"project definition",
				) {
					t.Fatal("skill catalog must contain file references, not skill bodies")
				}
				// Invoking a known command must not read or inline the skill body.
				if err := filesystem.Remove("/project/.agents/skills/review/SKILL.md"); err != nil {
					t.Fatal(err)
				}
				commands := catalog.available()
				if len(commands) != 2 || commands[0].Name != "global" || commands[1].Name != "review" {
					t.Fatalf(
						"commands: %#v",
						commands,
					)
				}
				message := catalog.expand(
					gai.Message{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock(test.prompt),
						},
					},
				)
				if !strings.Contains(
					message.Blocks[0].Content.String(),
					test.want,
				) {
					t.Fatalf(
						"expanded prompt: %s",
						message.Blocks[0].Content.String(),
					)
				}
			},
		)
	}
}

func TestPluginServers(t *testing.T) {
	for _, test := range []struct {
		name        string
		command     string
		cwd         string
		wantCommand string
		wantCwd     string
		wantError   bool
	}{
		{
			name:        "root expansion",
			command:     "${PLUGIN_ROOT}/bin/server",
			wantCommand: "/plugins/foo/bin/server",
			wantCwd:     "/plugins/foo",
		},
		{
			name:        "relative executable",
			command:     "./bin/server",
			cwd:         "subdir",
			wantCommand: "/plugins/foo/bin/server",
			wantCwd:     "/plugins/foo/subdir",
		},
		{
			name:      "unsupported data placeholder",
			command:   "server",
			cwd:       "${PLUGIN_DATA}",
			wantError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				catalog := commandCatalog{
					plugins: []plugins.Plugin{
						{
							Name: "foo",
							Path: "/plugins/foo",
							MCPConfig: map[string]plugins.MCPServer{
								"server": {
									Type:    plugins.Stdio,
									Command: test.command,
									Cwd:     test.cwd,
								},
							},
						},
					},
				}
				pluginServers := catalog.pluginServers()
				if len(pluginServers["foo"]) != 1 || pluginServers["foo"][0] != "foo:server" {
					t.Fatalf(
						"plugin server catalog: %v",
						pluginServers,
					)
				}
				if !strings.Contains(
					catalog.instructions(),
					"- foo:",
				) {
					t.Fatal("MCP-only plugin is missing from model instructions")
				}
				servers, err := catalog.servers(
					nil,
					"/work",
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"servers: %v",
						err,
					)
				}
				if err != nil {
					return
				}
				server := servers["foo:server"]
				if server.Command != test.wantCommand || server.Cwd != test.wantCwd {
					t.Fatalf(
						"resolved server: %q %q",
						server.Command,
						server.Cwd,
					)
				}
			},
		)
	}
}
