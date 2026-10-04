package skills_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

func TestInstall(t *testing.T) {
	outputErr := errors.New("output unavailable")
	for _, test := range []struct {
		name      string
		scope     skills.ScopeOptions
		source    string
		names     []string
		deep      bool
		link      bool
		json      bool
		errText   string
		outputErr error
	}{
		{
			name: "custom project directory",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
		},
		{
			name: "global directory",
			scope: skills.ScopeOptions{
				Global: true,
			},
			source: managerSource,
		},
		{
			name: "selected skill only",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
		},
		{
			name: "full depth forwarded",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			deep:   true,
		},
		{
			name: "JSON result",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			json:   true,
		},
		{
			name: "output failure preserves its cause and diagnostic",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:    managerSource,
			outputErr: outputErr,
		},
		{
			name:    "scope required",
			source:  managerSource,
			errText: "scope",
		},
		{
			name: "symlink followed by parent component is not cleaned away",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/link/../custom"),
			},
			source:  managerSource,
			link:    true,
			errText: "symlink",
		},
		{
			name: "missing component cannot hide link behind parent traversal",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/missing/../link/custom"),
			},
			source:  managerSource,
			link:    true,
			errText: "parent",
		},
		{
			name: "source required",
			scope: skills.ScopeOptions{
				Project: true,
			},
			errText: "source",
		},
		{
			name: "conflicting scopes",
			scope: skills.ScopeOptions{
				Project: true,
				Global:  true,
			},
			source:  managerSource,
			errText: "scope",
		},
		{
			name: "global and custom directory conflict",
			scope: skills.ScopeOptions{
				Global:    true,
				Directory: new("/custom"),
			},
			source:  managerSource,
			errText: "directory",
		},
		{
			name: "empty explicit directory",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new(""),
			},
			source:  managerSource,
			errText: "directory",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"HOME",
					"/test-home",
				)
				destinations := newManagerFilesystem(
					t,
					"/",
					nil,
				)
				if test.link {
					if err := destinations.MkdirAll(
						"/link",
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					destinations = managerLinkFS{
						Fs: destinations,
						links: map[string]bool{
							"/link": true,
						},
					}
				}
				files := fstest.MapFS{
					"alpha/SKILL.md": {
						Data: []byte(alphaSkill),
					},
					"beta/SKILL.md": {
						Data: []byte(betaSkill),
					},
				}
				if test.deep {
					files = fstest.MapFS{
						"a/b/c/d/e/alpha/SKILL.md": {
							Data: []byte(alphaSkill),
						},
					}
				}
				before := snapshotManagerFS(
					t,
					destinations,
				)
				manager := skills.NewManager(
					managerSourceClient{
						files: files,
					},
					destinations,
				)
				var output bytes.Buffer
				streams := skills.IO{
					Out: &output,
					Err: &output,
				}
				if test.outputErr != nil {
					streams.Out = commandOutputFailure{
						err: test.outputErr,
					}
				}
				err := skills.Install(
					t.Context(),
					manager,
					skills.InstallOptions{
						Scope:     test.scope,
						Source:    test.source,
						Skills:    test.names,
						FullDepth: test.deep,
						JSON:      test.json,
					},
					streams,
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"error = %v, want %q",
							err,
							test.errText,
						)
					}
					if diff := cmp.Diff(
						before,
						snapshotManagerFS(
							t,
							destinations,
						),
					); diff != "" {
						t.Fatalf(
							"invalid invocation changed files:\n%s",
							diff,
						)
					}
					return
				}
				if test.outputErr != nil {
					if !errors.Is(
						err,
						test.outputErr,
					) || err.Error() != "output unavailable" {
						t.Fatalf(
							"error = %v, want output failure with its original diagnostic",
							err,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				root := "/custom"
				if test.scope.Global {
					root = filepath.FromSlash("/test-home/.agents/skills")
				}
				data, readErr := afero.ReadFile(
					destinations,
					filepath.Join(
						root,
						"alpha",
						"SKILL.md",
					),
				)
				if readErr != nil || string(data) != alphaSkill {
					t.Errorf(
						"installation at selected root = %q, %v",
						data,
						readErr,
					)
				}
				if len(test.names) > 0 {
					if _, statErr := destinations.Stat(filepath.Join(
						root,
						"beta",
					)); !errors.Is(
						statErr,
						fs.ErrNotExist,
					) {
						t.Errorf(
							"unselected beta installed: %v",
							statErr,
						)
					}
				}
				if test.outputErr != nil {
					return
				}
				if test.json {
					if !json.Valid(output.Bytes()) {
						t.Fatalf(
							"invalid JSON: %q",
							&output,
						)
					}
				} else if !strings.Contains(
					output.String(),
					"alpha",
				) {
					t.Errorf(
						"missing installed name in output: %q",
						&output,
					)
				}
			},
		)
	}
}

type commandOutputFailure struct {
	err error
}

func (writer commandOutputFailure) Write([]byte) (int, error) {
	return 0, writer.err
}

func TestList(t *testing.T) {
	for _, test := range []struct {
		name              string
		scope             skills.ScopeOptions
		source            *string
		json              bool
		want              []string
		unsafeDescription bool
		nestedMetadata    bool
		nonJSONMetadata   bool
	}{
		{
			name: "both scopes by default",
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name: "project only",
			scope: skills.ScopeOptions{
				Project: true,
			},
			want: []string{
				"alpha",
			},
		},
		{
			name:              "terminal controls escaped",
			unsafeDescription: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name: "global only",
			scope: skills.ScopeOptions{
				Global: true,
			},
			want: []string{
				"beta",
			},
		},
		{
			name: "custom directory excludes both defaults",
			scope: skills.ScopeOptions{
				Directory: new("/custom"),
			},
			want: []string{
				"gamma",
			},
		},
		{
			name:   "source catalog",
			source: new(managerSource),
			want: []string{
				"delta",
			},
		},
		{
			name: "JSON inventory",
			json: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:           "nested metadata text inventory",
			nestedMetadata: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:           "nested metadata JSON inventory",
			nestedMetadata: true,
			json:           true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:            "non-JSON metadata text inventory",
			nonJSONMetadata: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:            "non-JSON metadata reports a JSON encoding error",
			nonJSONMetadata: true,
			json:            true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"HOME",
					"/test-home",
				)
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				definition := alphaSkill
				if test.nestedMetadata {
					definition = "---\nname: alpha\ndescription: Alpha skill\nhomepage: https://browser-use.com\nmetadata:\n  openclaw:\n    requires:\n      bins: [browser-use]\n    values: [42, true, null]\n---\n"
				}
				if test.nonJSONMetadata {
					definition = "---\nname: alpha\ndescription: Alpha skill\nmetadata:\n  value: .nan\n---\n"
				}
				if test.unsafeDescription {
					definition = strings.ReplaceAll(
						alphaSkill,
						"Alpha skill",
						`"Alpha\e]52;c;evil\a"`,
					)
				}
				destinations := newManagerFilesystem(
					t,
					skills.ProjectPath(cwd),
					fstest.MapFS{
						"alpha/SKILL.md": {
							Data: []byte(definition),
						},
					},
				)
				for root, files := range map[string]fstest.MapFS{
					filepath.FromSlash("/test-home/.agents/skills"): {
						"beta/SKILL.md": {
							Data: []byte(betaSkill),
						},
					},
					"/custom": {
						"gamma/SKILL.md": {
							Data: []byte("---\nname: gamma\ndescription: Gamma\n---\n"),
						},
					},
				} {
					for name, file := range files {
						filename := filepath.Join(
							root,
							filepath.FromSlash(name),
						)
						if err := destinations.MkdirAll(
							filepath.Dir(filename),
							0o700,
						); err != nil {
							t.Fatal(err)
						}
						if err := afero.WriteFile(
							destinations,
							filename,
							file.Data,
							0o600,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
				manager := skills.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							"delta/SKILL.md": {
								Data: []byte("---\nname: delta\ndescription: \"Delta\\e]52;c;evil\\a\"\n---\n"),
							},
						},
					},
					destinations,
				)
				var output bytes.Buffer
				err = skills.List(
					t.Context(),
					manager,
					skills.ListOptions{
						Scope:  test.scope,
						Source: test.source,
						JSON:   test.json,
					},
					skills.IO{
						Out: &output,
					},
				)
				if test.nonJSONMetadata && test.json {
					var unsupported *json.UnsupportedValueError
					if !errors.As(
						err,
						&unsupported,
					) {
						t.Fatalf(
							"error = %v, want unsupported JSON value",
							err,
						)
					}
					if output.Len() != 0 {
						t.Fatalf(
							"failed JSON encoding wrote partial output: %q",
							&output,
						)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if strings.ContainsAny(
					output.String(),
					"\x1b\x07\r",
				) {
					t.Errorf(
						"untrusted controls reached terminal: %q",
						&output,
					)
				}
				for _, name := range []string{
					"alpha",
					"beta",
					"gamma",
					"delta",
				} {
					present := false
					for _, want := range test.want {
						if name == want {
							present = true
						}
					}
					if strings.Contains(
						output.String(),
						name,
					) != present {
						t.Errorf(
							"name %s presence = %t, output=%q",
							name,
							present,
							&output,
						)
					}
				}
				if test.json && !json.Valid(output.Bytes()) {
					t.Errorf(
						"invalid JSON: %q",
						&output,
					)
				}
				if test.nestedMetadata && test.json {
					var entries []skills.InstalledSkill
					if err := json.Unmarshal(
						output.Bytes(),
						&entries,
					); err != nil {
						t.Fatal(err)
					}
					if len(entries) != 2 || entries[0].Skill.Name != "alpha" {
						t.Fatalf(
							"inventory = %#v",
							entries,
						)
					}
					if diff := cmp.Diff(
						map[string]any{
							"openclaw": map[string]any{
								"requires": map[string]any{
									"bins": []any{
										"browser-use",
									},
								},
								"values": []any{
									float64(42),
									true,
									nil,
								},
							},
						},
						entries[0].Skill.Metadata,
					); diff != "" {
						t.Errorf(
							"JSON metadata (-want +got):\n%s",
							diff,
						)
					}
				}
			},
		)
	}
}

func TestRemove(t *testing.T) {
	for _, test := range []struct {
		name              string
		scope             skills.ScopeOptions
		both, json        bool
		invalidCrossScope bool
		errText           string
	}{
		{
			name: "unambiguous default selection",
		},
		{
			name:              "cross scope invalid selection preserves valid selection",
			invalidCrossScope: true,
			errText:           "skill directory",
		},
		{
			name:    "ambiguous default selection",
			both:    true,
			errText: "scope",
		},
		{
			name: "explicit project preserves global",
			scope: skills.ScopeOptions{
				Project: true,
			},
			both: true,
		},
		{
			name: "custom directory only",
			scope: skills.ScopeOptions{
				Directory: new("/custom"),
			},
			both: true,
		},
		{
			name: "JSON result",
			json: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"HOME",
					"/test-home",
				)
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				project := skills.ProjectPath(cwd)
				global := filepath.FromSlash("/test-home/.agents/skills")
				destinations := newManagerFilesystem(
					t,
					project,
					fstest.MapFS{
						"alpha/SKILL.md": {
							Data: []byte(alphaSkill),
						},
					},
				)
				if test.both {
					if err := destinations.MkdirAll(
						filepath.Join(
							global,
							"alpha",
						),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						destinations,
						filepath.Join(
							global,
							"alpha",
							"SKILL.md",
						),
						[]byte(alphaSkill),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				if test.scope.Directory != nil {
					if err := destinations.MkdirAll(
						"/custom/alpha",
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						destinations,
						"/custom/alpha/SKILL.md",
						[]byte(alphaSkill),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				names := []string{
					"alpha",
				}
				if test.invalidCrossScope {
					names = append(
						names,
						"notes",
					)
					if err := destinations.MkdirAll(
						filepath.Join(
							global,
							"notes",
						),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						destinations,
						filepath.Join(
							global,
							"notes",
							"file.txt",
						),
						[]byte("keep"),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				before := snapshotManagerFS(
					t,
					destinations,
				)
				var output bytes.Buffer
				err = skills.Remove(
					t.Context(),
					skills.NewManager(
						nil,
						destinations,
					),
					skills.RemoveOptions{
						Scope:  test.scope,
						Skills: names,
						JSON:   test.json,
					},
					skills.IO{
						Out: &output,
					},
				)
				if test.errText != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.errText,
					) {
						t.Fatalf(
							"error = %v",
							err,
						)
					}
					if diff := cmp.Diff(
						before,
						snapshotManagerFS(
							t,
							destinations,
						),
					); diff != "" {
						t.Fatal(diff)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				selected := project
				if test.scope.Directory != nil {
					selected = *test.scope.Directory
				}
				if _, err := destinations.Stat(filepath.Join(
					selected,
					"alpha",
				)); !errors.Is(
					err,
					fs.ErrNotExist,
				) {
					t.Errorf(
						"selected bundle still exists: %v",
						err,
					)
				}
				if test.both {
					if _, err := destinations.Stat(filepath.Join(
						global,
						"alpha",
					)); err != nil {
						t.Errorf(
							"global counterpart changed: %v",
							err,
						)
					}
				}
				if test.scope.Directory != nil {
					if _, err := destinations.Stat(filepath.Join(
						project,
						"alpha",
					)); err != nil {
						t.Errorf(
							"default project changed: %v",
							err,
						)
					}
				}
				if test.json && !json.Valid(output.Bytes()) {
					t.Error("invalid JSON")
				}
			},
		)
	}
}

func TestUpdate(t *testing.T) {
	for _, test := range []struct {
		name                                string
		scope                               skills.ScopeOptions
		json                                bool
		names                               []string
		wantProject, wantGlobal, wantCustom bool
	}{
		{
			name:        "both scopes by default",
			wantProject: true,
			wantGlobal:  true,
		},
		{
			name: "selected skill only",
			names: []string{
				"alpha",
			},
			wantProject: true,
			wantGlobal:  true,
		},
		{
			name: "project only",
			scope: skills.ScopeOptions{
				Project: true,
			},
			wantProject: true,
		},
		{
			name: "global only",
			scope: skills.ScopeOptions{
				Global: true,
			},
			wantGlobal: true,
		},
		{
			name: "custom directory only",
			scope: skills.ScopeOptions{
				Directory: new("/custom"),
			},
			wantCustom: true,
		},
		{
			name:        "JSON result",
			json:        true,
			wantProject: true,
			wantGlobal:  true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"HOME",
					"/test-home",
				)
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				project := skills.ProjectPath(cwd)
				global := filepath.FromSlash("/test-home/.agents/skills")
				destinations := afero.NewMemMapFs()
				for _, root := range []string{
					project,
					global,
					"/custom",
				} {
					for name, data := range map[string]string{
						"alpha/SKILL.md": alphaSkill,
						"beta/SKILL.md":  betaSkill,
						"skills.json":    bothRecords,
					} {
						filename := filepath.Join(
							root,
							filepath.FromSlash(name),
						)
						if err := destinations.MkdirAll(
							filepath.Dir(filename),
							0o700,
						); err != nil {
							t.Fatal(err)
						}
						if err := afero.WriteFile(
							destinations,
							filename,
							[]byte(data),
							0o600,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
				manager := skills.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							"alpha/SKILL.md": {
								Data: []byte(alphaUpdated),
							},
							"beta/SKILL.md": {
								Data: []byte(strings.ReplaceAll(
									betaSkill,
									"Beta skill",
									"Updated beta",
								)),
							},
						},
					},
					destinations,
				)
				var output bytes.Buffer
				err = skills.Update(
					t.Context(),
					manager,
					skills.UpdateOptions{
						Scope:  test.scope,
						Skills: test.names,
						JSON:   test.json,
					},
					skills.IO{
						Out: &output,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				for _, root := range []struct {
					path    string
					updated bool
				}{
					{
						project,
						test.wantProject,
					},
					{
						global,
						test.wantGlobal,
					},
					{
						"/custom",
						test.wantCustom,
					},
				} {
					want := alphaSkill
					if root.updated {
						want = alphaUpdated
					}
					got, readErr := afero.ReadFile(
						destinations,
						filepath.Join(
							root.path,
							"alpha",
							"SKILL.md",
						),
					)
					if readErr != nil || string(got) != want {
						t.Errorf(
							"%s content = %q, %v",
							root.path,
							got,
							readErr,
						)
					}
					wantBeta := betaSkill
					if root.updated && len(test.names) == 0 {
						wantBeta = strings.ReplaceAll(
							betaSkill,
							"Beta skill",
							"Updated beta",
						)
					}
					beta, betaErr := afero.ReadFile(
						destinations,
						filepath.Join(
							root.path,
							"beta",
							"SKILL.md",
						),
					)
					if betaErr != nil || string(beta) != wantBeta {
						t.Errorf(
							"%s beta = %q, %v",
							root.path,
							beta,
							betaErr,
						)
					}
				}
				if test.json && !json.Valid(output.Bytes()) {
					t.Error("invalid JSON")
				}
			},
		)
	}
}
