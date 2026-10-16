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
		name      string
		prompt    string
		want      string
		wantError bool
	}{
		{
			name:   "project overrides global",
			prompt: "/review changes",
			want:   "project definition",
		},
		{
			name:   "global remains available",
			prompt: "/global context",
			want:   "global definition",
		},
		{
			name:      "unknown command",
			prompt:    "/missing",
			wantError: true,
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
				commands := catalog.available()
				if len(commands) != 2 || commands[0].Name != "global" || commands[1].Name != "review" {
					t.Fatalf(
						"commands: %#v",
						commands,
					)
				}
				message, err := catalog.expand(
					filesystem,
					gai.Message{
						Role: gai.User,
						Blocks: []gai.Block{
							gai.TextBlock(test.prompt),
						},
					},
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"expand: %v",
						err,
					)
				}
				if err == nil && !strings.Contains(
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
