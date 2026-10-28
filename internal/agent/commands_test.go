package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/mcpclient"
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
			name:   "compact command takes precedence over a skill",
			prompt: "/compact",
			want:   "Compact the conversation now by calling compact(summary) in starlark_exec.",
		},
		{
			name:   "compact request preserves user notes",
			prompt: "/compact retain the next steps",
			want:   "User request:\nretain the next steps",
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
					"/global/compact/SKILL.md":                "---\nname: compact\ndescription: A skill with a reserved name\n---\nunused skill body",
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
				empty, err := mcpclient.NewCatalog(
					nil,
					nil,
				)
				if err != nil {
					t.Fatal(err)
				}
				instructions, err := catalog.extensionInstructions(empty)
				if err != nil {
					t.Fatal(err)
				}
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
				if len(commands) != 3 || commands[0].Name != "compact" || commands[1].Name != "global" || commands[2].Name != "review" {
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
		wantSkipped bool
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
			name:        "unsupported data placeholder",
			command:     "server",
			cwd:         "${PLUGIN_DATA}",
			wantSkipped: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				catalog := commandCatalog{
					plugins: []plugins.Plugin{
						{
							Name:        "foo",
							Description: "Foo tools",
							Path:        "/plugins/foo",
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
				specs, failures, err := catalog.pluginSpecs()
				if err != nil {
					t.Fatal(err)
				}
				if test.wantSkipped {
					if len(specs) != 0 || len(failures) != 1 || failures[0].Spec.ID != "foo:server" || !strings.Contains(
						failures[0].Err.Error(),
						"unsupported placeholder",
					) {
						t.Fatalf(
							"specs = %+v, failures = %+v",
							specs,
							failures,
						)
					}
					return
				}
				if len(specs) != 1 || specs[0].ID != "foo:server" || specs[0].Source != mcpclient.SourcePlugin || specs[0].Plugin != "foo" {
					t.Fatalf(
						"specs: %+v",
						specs,
					)
				}
				server := specs[0].Transport
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
