package plugins_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/plugins"
)

type commandOutputFailure struct{ err error }

func (writer commandOutputFailure) Write([]byte) (int, error) { return 0, writer.err }

func TestInstall(t *testing.T) {
	outputErr := errors.New("output unavailable")
	for _, test := range []struct {
		outputErr     error
		wantErr       error
		name          string
		scope         commandio.ScopeOptions
		wantDirectory string
		errText       string
		terminal      bool
		pipe          bool
		json          bool
		empty         bool
		fault         managerFSFault
	}{
		{
			name: "project default automatic JSON",
			scope: commandio.ScopeOptions{
				Project: true,
			},
		},
		{
			name: "global default",
			scope: commandio.ScopeOptions{
				Global: true,
			},
		},
		{
			name: "exact relative custom directory",
			scope: commandio.ScopeOptions{
				Project:   true,
				Directory: new("custom"),
			},
			wantDirectory: "custom",
		},
		{
			name: "terminal table",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			terminal: true,
		},
		{
			name: "explicit JSON on terminal",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			terminal: true,
			json:     true,
		},
		{
			name: "pipe stays JSON with forced color",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			pipe: true,
		},
		{
			name: "empty results are an array not null",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			empty: true,
		},
		{
			name: "scope required even for custom directory",
			scope: commandio.ScopeOptions{
				Directory: new("custom"),
			},
			errText: "scope is required",
		},
		{
			name: "conflicting scopes",
			scope: commandio.ScopeOptions{
				Project: true,
				Global:  true,
			},
			errText: "conflicting scope",
		},
		{
			name: "custom global is rejected",
			scope: commandio.ScopeOptions{
				Global:    true,
				Directory: new("custom"),
			},
			errText: "global scope",
		},
		{
			name: "empty directory rejected",
			scope: commandio.ScopeOptions{
				Project:   true,
				Directory: new(""),
			},
			errText: "must not be empty",
		},
		{
			name: "cleanup failure still emits committed installation",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			fault:   managerStageCleanupFailure,
			errText: "changes committed",
		},
		{
			name: "JSON writer error preserves its cause",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			outputErr: outputErr,
			wantErr:   outputErr,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				cwd := t.TempDir()
				t.Chdir(cwd)
				home := t.TempDir()
				t.Setenv(
					"HOME",
					home,
				)
				filesystem := afero.NewMemMapFs()
				files := fstest.MapFS{}
				if !test.empty {
					files["plugin.json"] = &fstest.MapFile{
						Data: []byte(alphaPlugin),
					}
				}
				manager := plugins.NewManager(
					managerSourceClient{
						files: files,
					},
					managerFaultFS{
						Fs:    filesystem,
						fault: test.fault,
					},
				)
				var output bytes.Buffer
				var writer io.Writer = &output
				if test.terminal {
					writer = commandTerminal(
						t,
						&output,
					)
				}
				if test.pipe {
					t.Setenv(
						"CLICOLOR_FORCE",
						"1",
					)
					writer = commandPipe(
						t,
						&output,
					)
				}
				if test.outputErr != nil {
					writer = commandOutputFailure{
						err: test.outputErr,
					}
				}
				err := plugins.Install(
					t.Context(),
					manager,
					plugins.InstallOptions{
						Scope:  test.scope,
						Source: managerSource,
						JSON:   test.json,
					},
					commandio.IO{
						Out: writer,
						Err: io.Discard,
					},
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"Install error = %v, want %q",
							err,
							test.errText,
						)
					}
					if test.fault == managerNoFault {
						if output.Len() != 0 {
							t.Fatal("invalid invocation emitted results")
						}
						return
					}
				} else if test.wantErr != nil {
					if !errors.Is(
						err,
						test.wantErr,
					) {
						t.Fatal("output failure cause was lost")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				directory := filepath.Join(
					cwd,
					".agents",
					"plugins",
				)
				if test.scope.Global {
					directory = filepath.Join(
						home,
						".agents",
						"plugins",
					)
				} else if test.wantDirectory != "" {
					directory = filepath.Join(
						cwd,
						test.wantDirectory,
					)
				}
				if !test.empty {
					if _, err := filesystem.Stat(filepath.Join(
						directory,
						"alpha",
						"plugin.json",
					)); err != nil {
						t.Fatal("installation missed the selected exact directory")
					}
				}
				if test.outputErr != nil {
					return
				}
				if test.terminal && !test.json {
					if !strings.Contains(
						output.String(),
						"Plugin",
					) || !strings.Contains(
						output.String(),
						"Alpha plugin",
					) {
						t.Fatal("missing terminal table")
					}
					return
				}
				var entries []map[string]string
				if err := json.Unmarshal(
					output.Bytes(),
					&entries,
				); err != nil || entries == nil {
					t.Fatalf(
						"output is not a JSON array: %q, %v",
						&output,
						err,
					)
				}
				if test.empty {
					if len(entries) != 0 {
						t.Fatal("unexpected empty-source results")
					}
				} else if len(entries) != 1 || len(entries[0]) != 3 || entries[0]["name"] != "alpha" || entries[0]["path"] != filepath.Join(
					directory,
					"alpha",
				) {
					t.Fatalf(
						"installation summaries = %#v",
						entries,
					)
				}
			},
		)
	}
}

func TestList(t *testing.T) {
	for _, test := range []struct {
		name        string
		source      bool
		empty       bool
		terminal    bool
		json        bool
		diagnostics bool
	}{
		{
			name: "installed default scopes automatic JSON",
		},
		{
			name:   "source paths stay source-relative",
			source: true,
		},
		{
			name:  "empty inventory is an array",
			empty: true,
		},
		{
			name:     "terminal cells strip hostile controls",
			terminal: true,
		},
		{
			name:     "explicit JSON on terminal",
			terminal: true,
			json:     true,
		},
		{
			name:        "component diagnostics go to stderr without corrupting JSON",
			diagnostics: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				cwd := t.TempDir()
				t.Chdir(cwd)
				t.Setenv(
					"HOME",
					t.TempDir(),
				)
				filesystem := afero.NewMemMapFs()
				root := filepath.Join(
					cwd,
					".agents",
					"plugins",
				)
				manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"alpha","description":"Cost\u001b[2J guide"}`
				files := fstest.MapFS{}
				if !test.empty {
					files["nested/plugin.json"] = &fstest.MapFile{
						Data: []byte(manifest),
					}
					if test.diagnostics {
						files["nested/mcp.json"] = &fstest.MapFile{
							Data: []byte("invalid"),
						}
					}
					for name, file := range files {
						filename := filepath.Join(
							root,
							filepath.FromSlash(name),
						)
						if err := filesystem.MkdirAll(
							filepath.Dir(filename),
							0o700,
						); err != nil {
							t.Fatal(err)
						}
						if err := afero.WriteFile(
							filesystem,
							filename,
							file.Data,
							0o600,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
				manager := plugins.NewManager(
					managerSourceClient{
						files: files,
					},
					filesystem,
				)
				var output, diagnostics bytes.Buffer
				var writer io.Writer = &output
				if test.terminal {
					writer = commandTerminal(
						t,
						&output,
					)
				}
				var source *string
				if test.source {
					source = new(managerSource)
				}
				if err := plugins.List(
					t.Context(),
					manager,
					plugins.ListOptions{
						Source: source,
						JSON:   test.json,
					},
					commandio.IO{
						Out: writer,
						Err: &diagnostics,
					},
				); err != nil {
					t.Fatal(err)
				}
				if test.diagnostics && !strings.Contains(
					diagnostics.String(),
					"mcp.json",
				) {
					t.Fatal("component diagnostic not reported separately")
				}
				if test.terminal && !test.json {
					if strings.Contains(
						output.String(),
						"[2J",
					) || !strings.Contains(
						output.String(),
						"Cost guide",
					) {
						t.Fatal("hostile table cell was not sanitized")
					}
					return
				}
				var entries []map[string]string
				if err := json.Unmarshal(
					output.Bytes(),
					&entries,
				); err != nil || entries == nil {
					t.Fatal("listing is not a JSON array")
				}
				if test.empty {
					if len(entries) != 0 {
						t.Fatal("unexpected empty inventory results")
					}
					return
				}
				wantPath := filepath.Join(
					root,
					"nested",
				)
				if test.source {
					wantPath = "nested"
				}
				if len(entries) != 1 || len(entries[0]) != 3 || entries[0]["path"] != wantPath || entries[0]["description"] != "Cost\x1b[2J guide" {
					t.Fatalf(
						"listing summaries = %#v",
						entries,
					)
				}
			},
		)
	}
}

func TestRemove(t *testing.T) {
	for _, test := range []struct {
		name     string
		scope    commandio.ScopeOptions
		errText  string
		both     bool
		all      bool
		noNames  bool
		terminal bool
		json     bool
		fault    managerFSFault
	}{
		{
			name: "unambiguous default scopes automatic JSON",
		},
		{
			name:    "ambiguous default scopes rejected before mutation",
			both:    true,
			errText: "both directories",
		},
		{
			name: "explicit project preserves global copy",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			both: true,
		},
		{
			name:     "terminal removal table",
			terminal: true,
		},
		{
			name:     "explicit JSON on terminal",
			terminal: true,
			json:     true,
		},
		{
			name:    "all selects actual installed directories",
			all:     true,
			noNames: true,
		},
		{
			name:    "empty selection rejected",
			noNames: true,
			errText: "at least one plugin",
		},
		{
			name:    "all and names cannot be combined",
			all:     true,
			errText: "all cannot be combined",
		},
		{
			name:    "committed removal output survives cleanup failure",
			fault:   managerStageCleanupFailure,
			errText: "changes committed",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				cwd := t.TempDir()
				t.Chdir(cwd)
				home := t.TempDir()
				t.Setenv(
					"HOME",
					home,
				)
				filesystem := afero.NewMemMapFs()
				project := filepath.Join(
					cwd,
					".agents",
					"plugins",
				)
				global := filepath.Join(
					home,
					".agents",
					"plugins",
				)
				roots := []string{
					project,
				}
				if test.both {
					roots = append(
						roots,
						global,
					)
				}
				for _, root := range roots {
					if err := filesystem.MkdirAll(
						filepath.Join(
							root,
							"manual",
						),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						filesystem,
						filepath.Join(
							root,
							"manual",
							"plugin.json",
						),
						[]byte(alphaPlugin),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				manager := plugins.NewManager(
					nil,
					managerFaultFS{
						Fs:    filesystem,
						fault: test.fault,
					},
				)
				names := []string{
					"manual",
				}
				if test.noNames {
					names = nil
				}
				var output bytes.Buffer
				var writer io.Writer = &output
				if test.terminal {
					writer = commandTerminal(
						t,
						&output,
					)
				}
				err := plugins.Remove(
					t.Context(),
					manager,
					plugins.RemoveOptions{
						Scope:   test.scope,
						Plugins: names,
						All:     test.all,
						JSON:    test.json,
					},
					commandio.IO{
						Out: writer,
					},
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"Remove error = %v, want %q",
							err,
							test.errText,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				_, statErr := filesystem.Stat(filepath.Join(
					project,
					"manual",
				))
				if test.errText != "" && test.fault == managerNoFault {
					if statErr != nil || output.Len() != 0 {
						t.Fatal("invalid removal changed state or emitted results")
					}
					return
				}
				if !errors.Is(
					statErr,
					fs.ErrNotExist,
				) {
					t.Fatal("selected package remains")
				}
				if test.both {
					if _, err := filesystem.Stat(filepath.Join(
						global,
						"manual",
					)); err != nil {
						t.Fatal("unselected global package changed")
					}
				}
				if test.terminal && !test.json {
					if !strings.Contains(
						output.String(),
						"manual",
					) {
						t.Fatal("missing removal table")
					}
					return
				}
				var entries []map[string]string
				if err := json.Unmarshal(
					output.Bytes(),
					&entries,
				); err != nil || len(entries) != 1 || entries[0]["name"] != "alpha" || entries[0]["path"] != filepath.Join(
					project,
					"manual",
				) {
					t.Fatalf(
						"removal summaries = %#v, %v",
						entries,
						err,
					)
				}
			},
		)
	}
}
