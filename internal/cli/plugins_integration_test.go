package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/plugins"
	acquisition "github.com/spachava753/cpe/internal/source"
)

func TestPluginsLifecycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	for key, value := range map[string]string{
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_CONFIG_GLOBAL":     os.DevNull,
		"GIT_CONFIG_COUNT":      "0",
		"GIT_CONFIG_PARAMETERS": "",
	} {
		t.Setenv(
			key,
			value,
		)
	}
	repository := t.TempDir()
	marker := filepath.Join(
		t.TempDir(),
		"code-must-not-run",
	)
	packagePath := "providers/agent-plugins/plugin"
	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"stripe","description":"Stripe integrations","extensions":{"com.openai":{"metadata":"preserve me"}}}`
	mcp := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"stripe":{"type":"streamable-http","url":"https://mcp.stripe.com"},"never-run":{"type":"stdio","command":"./bin/never-run"}}}`
	skill := "---\nname: stripe-guide\ndescription: Build Stripe integrations\n---\nInstructions\n"
	badSkill := "---\nname: stripe-docs\ndescription: Stripe docs\nallowed-tools: [Read, Write]\n---\nInstructions\n"
	files := map[string]string{
		packagePath + "/plugin.json":                  manifest,
		packagePath + "/mcp.json":                     mcp,
		packagePath + "/skills/stripe-guide/SKILL.md": skill,
		packagePath + "/skills/stripe-docs/SKILL.md":  badSkill,
		packagePath + "/assets/help.txt":              "bundled resource",
		packagePath + "/bin/never-run":                "#!/bin/sh\ntouch '" + marker + "'\n",
		packagePath + "/.codex-plugin/plugin.json":    `{"name":"stripe"}`,
		"providers/legacy/.codex-plugin/plugin.json":  `{"name":"stripe"}`,
	}
	for name, data := range files {
		filename := filepath.Join(
			repository,
			filepath.FromSlash(name),
		)
		if err := os.MkdirAll(
			filepath.Dir(filename),
			0o700,
		); err != nil {
			t.Fatal(err)
		}
		mode := fs.FileMode(0o600)
		if strings.HasSuffix(
			name,
			"never-run",
		) {
			mode = 0o755
		}
		if err := os.WriteFile(
			filename,
			[]byte(data),
			mode,
		); err != nil {
			t.Fatal(err)
		}
	}
	runGit := func(args ...string) {
		t.Helper()
		if output, err := exec.CommandContext(
			t.Context(),
			"git",
			args...,
		).CombinedOutput(); err != nil {
			t.Fatalf(
				"git %v: %v, %s",
				args,
				err,
				output,
			)
		}
	}
	runGit(
		"init",
		"--quiet",
		repository,
	)
	runGit(
		"-C",
		repository,
		"add",
		".",
	)
	runGit(
		"-C",
		repository,
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.com",
		"commit",
		"--quiet",
		"-m",
		"fixture",
	)
	source := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(repository),
	}).String()
	destination := filepath.Join(
		t.TempDir(),
		"plugins",
	)
	installedPath := filepath.Join(
		destination,
		"stripe",
	)
	filesystem := afero.NewMemMapFs()
	manager := plugins.NewManager(
		acquisition.GitClient{},
		filesystem,
	)
	for _, test := range []struct {
		name      string
		args      []string
		wantPath  string
		installed bool
		update    bool
		errText   string
	}{
		{
			name: "source listing recognizes only native package",
			args: []string{
				"plugins",
				"list",
				source,
			},
			wantPath: packagePath,
		},
		{
			name: "install Stripe by exact source path",
			args: []string{
				"plugins",
				"install",
				source,
				"--path",
				packagePath,
				"--scope",
				"project",
				"--dir",
				destination,
			},
			wantPath:  installedPath,
			installed: true,
		},
		{
			name: "list installed package",
			args: []string{
				"plugins",
				"ls",
				"--dir",
				destination,
			},
			wantPath:  installedPath,
			installed: true,
		},
		{
			name: "existing installation is preserved without override",
			args: []string{
				"plugins",
				"install",
				source,
				"--plugin",
				"stripe",
				"--scope",
				"project",
				"--dir",
				destination,
			},
			installed: true,
			errText:   "use --override",
		},
		{
			name: "override removes obsolete resources",
			args: []string{
				"plugins",
				"add",
				source,
				"--plugin",
				"stripe",
				"--scope",
				"project",
				"--dir",
				destination,
				"--override",
			},
			wantPath:  installedPath,
			installed: true,
			update:    true,
		},
		{
			name: "remove whole installed package",
			args: []string{
				"plugins",
				"rm",
				"stripe",
				"--dir",
				destination,
			},
			wantPath: installedPath,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.update {
					if err := os.Remove(filepath.Join(
						repository,
						filepath.FromSlash(packagePath),
						"assets",
						"help.txt",
					)); err != nil {
						t.Fatal(err)
					}
					runGit(
						"-C",
						repository,
						"add",
						"-A",
					)
					runGit(
						"-C",
						repository,
						"-c",
						"user.name=Test",
						"-c",
						"user.email=test@example.com",
						"commit",
						"--quiet",
						"-m",
						"update fixture",
					)
				}
				command := Root(
					"test",
					nil,
					manager,
					nil,
					auth.LoginOptions{},
				)
				command.SilenceErrors = true
				command.SilenceUsage = true
				var output, diagnostics bytes.Buffer
				command.SetOut(&output)
				command.SetErr(&diagnostics)
				command.SetArgs(test.args)
				err := command.ExecuteContext(t.Context())
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"command error = %v, want %q",
							err,
							test.errText,
						)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					var summaries []map[string]string
					if err := json.Unmarshal(
						output.Bytes(),
						&summaries,
					); err != nil || len(summaries) != 1 || len(summaries[0]) != 3 || summaries[0]["name"] != "stripe" || summaries[0]["path"] != test.wantPath {
						t.Fatalf(
							"command summaries = %#v, %v",
							summaries,
							err,
						)
					}
				}
				if _, err := os.Stat(marker); !errors.Is(
					err,
					fs.ErrNotExist,
				) {
					t.Fatal("installation activated bundled code")
				}
				if !test.installed {
					if _, err := filesystem.Stat(installedPath); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Fatal("unexpected installed package")
					}
					return
				}
				installed, err := manager.ListInstalled(
					t.Context(),
					destination,
				)
				if err != nil || len(installed) != 1 {
					t.Fatalf(
						"installed packages = %#v, %v",
						installed,
						err,
					)
				}
				plugin := installed[0].Plugin
				server := plugin.MCPConfig["stripe"]
				if server.Type != plugins.StreamableHTTP || server.URL.String() != "https://mcp.stripe.com" || len(plugin.Skills) != 1 || plugin.Skills[0].Path != filepath.Join(
					installedPath,
					"skills",
					"stripe-guide",
					"SKILL.md",
				) || len(plugin.Diagnostics) != 1 {
					t.Fatalf(
						"installed components = %#v",
						plugin,
					)
				}
				for name, want := range map[string]string{
					"plugin.json":                  manifest,
					"mcp.json":                     mcp,
					"skills/stripe-guide/SKILL.md": skill,
					"skills/stripe-docs/SKILL.md":  badSkill,
					".codex-plugin/plugin.json":    `{"name":"stripe"}`,
				} {
					data, err := afero.ReadFile(
						filesystem,
						filepath.Join(
							installedPath,
							filepath.FromSlash(name),
						),
					)
					if err != nil || string(data) != want {
						t.Fatalf(
							"bundle file %s = %q, %v",
							name,
							data,
							err,
						)
					}
				}
				info, err := filesystem.Stat(filepath.Join(
					installedPath,
					"bin",
					"never-run",
				))
				if err != nil || info.Mode().Perm() != 0o755 {
					t.Fatal("bundled executable mode was lost")
				}
				if test.update {
					if _, err := filesystem.Stat(filepath.Join(
						installedPath,
						"assets",
						"help.txt",
					)); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Fatal("obsolete resource remains after override")
					}
				}
			},
		)
	}
}
