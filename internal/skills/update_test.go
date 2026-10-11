package skills_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
)

func TestUpdate(t *testing.T) {
	for _, test := range []struct {
		name               string
		names              []string
		project            bool
		global             bool
		atHome             bool
		duplicate          bool
		untracked          bool
		unsupported        bool
		unsupportedLocator bool
		missingURL         bool
		unreadableOther    bool
		corrupt            bool
		fault              managerFSFault
		wantPaths          []string
		warnings           []string
		wantError          string
	}{
		{
			name: "all scopes include both inventories",
			wantPaths: []string{
				"project/alpha",
				"global/beta",
			},
		},
		{
			name:      "updating from home retains global tracking",
			atHome:    true,
			untracked: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"not in the lock file",
			},
		},
		{
			name:   "updating from home handles both logical inventories",
			atHome: true,
			wantPaths: []string{
				"project/alpha",
				"global/beta",
			},
			warnings: []string{
				"not in the lock file",
			},
		},
		{
			name:      "all scopes update duplicate names independently",
			duplicate: true,
			wantPaths: []string{
				"project/alpha",
				"global/alpha",
				"global/beta",
			},
		},
		{
			name: "ambiguous named skill warns and skips",
			names: []string{
				"alpha",
				"beta",
			},
			duplicate: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"alpha",
				"explicit --scope",
			},
		},
		{
			name: "explicit project scope resolves duplicate",
			names: []string{
				"alpha",
			},
			project:   true,
			duplicate: true,
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:   "global all restricts scope",
			global: true,
			wantPaths: []string{
				"global/beta",
			},
		},
		{
			name:    "missing scoped skill warns even if installed elsewhere",
			project: true,
			names: []string{
				"beta",
			},
			warnings: []string{
				"beta",
				"does not exist",
			},
		},
		{
			name: "missing name does not prevent another update",
			names: []string{
				"missing",
				"alpha",
			},
			wantPaths: []string{
				"project/alpha",
			},
			warnings: []string{
				"missing",
				"does not exist",
			},
		},
		{
			name:      "untracked skill warns in all mode",
			untracked: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"alpha",
				"not in the lock file",
			},
		},
		{
			name:      "untracked skill warns when named",
			untracked: true,
			names: []string{
				"alpha",
			},
			warnings: []string{
				"not in the lock file",
			},
		},
		{
			name:        "unsupported source warns without blocking supported skills",
			unsupported: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"unsupported",
			},
		},
		{
			name:               "unsupported Git locator warns without blocking a supported sibling",
			unsupportedLocator: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"unsupported recorded source",
			},
		},
		{
			name:       "generic Git source without URL is not guessed as GitHub",
			missingURL: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"unambiguous sourceUrl",
			},
		},
		{
			name: "named selection does not inspect unrelated definitions in either scope",
			names: []string{
				"alpha",
			},
			unreadableOther: true,
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:      "corrupt lock prevents mutations",
			corrupt:   true,
			wantError: "lock",
		},
		{
			name: "duplicate arguments update once",
			names: []string{
				"alpha",
				"alpha",
			},
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:      "bundle failure leaves lock unchanged",
			project:   true,
			fault:     managerPublishFailure,
			wantError: "permission",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				project := t.TempDir()
				home := t.TempDir()
				if test.atHome {
					project = home
				}
				t.Chdir(project)
				t.Setenv(
					"HOME",
					home,
				)
				t.Setenv(
					"USERPROFILE",
					home,
				)
				t.Setenv(
					"XDG_STATE_HOME",
					"",
				)
				projectRoot := filepath.Join(
					project,
					".agents",
					"skills",
				)
				globalRoot := filepath.Join(
					home,
					".agents",
					"skills",
				)
				projectLock := filepath.Join(
					project,
					"skills-lock.json",
				)
				globalLock := filepath.Join(
					home,
					".agents",
					".skill-lock.json",
				)
				filesystem := afero.NewMemMapFs()
				writeTrackingFixture(
					t,
					filesystem,
					filepath.Join(
						projectRoot,
						"alpha",
						"SKILL.md",
					),
					alphaSkill,
				)
				writeTrackingFixture(
					t,
					filesystem,
					filepath.Join(
						projectRoot,
						"alpha",
						"obsolete.txt",
					),
					"obsolete",
				)
				writeTrackingFixture(
					t,
					filesystem,
					filepath.Join(
						globalRoot,
						"beta",
						"SKILL.md",
					),
					betaSkill,
				)
				projectRecords := map[string]any{}
				if !test.untracked {
					sourceType := "git"
					if test.unsupported {
						sourceType = "node_modules"
					}
					projectRecords["alpha"] = map[string]any{
						"source":       managerSource,
						"sourceType":   sourceType,
						"sourceUrl":    managerSource,
						"skillPath":    "alpha/SKILL.md",
						"computedHash": "old",
						"extra":        "preserved",
					}
				}
				if test.unsupportedLocator {
					projectRecords["alpha"].(map[string]any)["sourceUrl"] = "git@github.com:owner/repo.git"
				}
				if test.missingURL {
					projectRecords["alpha"].(map[string]any)["source"] = "owner/repo"
					delete(
						projectRecords["alpha"].(map[string]any),
						"sourceUrl",
					)
				}
				if test.unreadableOther {
					for _, root := range []string{
						projectRoot,
						globalRoot,
					} {
						writeTrackingFixture(
							t,
							filesystem,
							filepath.Join(
								root,
								"ignored",
								"SKILL.md",
							),
							"invalid definition",
						)
					}
				}
				globalRecords := map[string]any{
					"beta": map[string]any{
						"source":          managerSource,
						"sourceType":      "git",
						"sourceUrl":       managerSource,
						"skillPath":       "beta/SKILL.md",
						"skillFolderHash": "old",
						"installedAt":     "2026-01-01T00:00:00.000Z",
					},
				}
				if test.duplicate {
					writeTrackingFixture(
						t,
						filesystem,
						filepath.Join(
							globalRoot,
							"alpha",
							"SKILL.md",
						),
						alphaSkill,
					)
					globalRecords["alpha"] = map[string]any{
						"source":          managerSource,
						"sourceType":      "git",
						"sourceUrl":       managerSource,
						"skillPath":       "alpha/SKILL.md",
						"skillFolderHash": "old",
					}
				}
				for filename, data := range map[string]any{
					projectLock: map[string]any{
						"version": 1,
						"skills":  projectRecords,
					},
					globalLock: map[string]any{
						"version": 3,
						"skills":  globalRecords,
						"dismissed": map[string]any{
							"findSkillsPrompt": true,
						},
					},
				} {
					encoded, err := json.Marshal(data)
					if err != nil {
						t.Fatal(err)
					}
					writeTrackingFixture(
						t,
						filesystem,
						filename,
						string(encoded),
					)
				}
				if test.corrupt {
					writeTrackingFixture(
						t,
						filesystem,
						projectLock,
						"invalid JSON",
					)
				}
				before := snapshotManagerFS(
					t,
					filesystem,
				)
				manager := skills.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							"alpha/SKILL.md": {
								Data: []byte(alphaUpdated),
							},
							"beta/SKILL.md": {
								Data: []byte(betaSkill),
							},
						},
					},
					updateInventoryFS{
						Fs: managerFaultFS{
							Fs:    filesystem,
							fault: test.fault,
						},
						unreadableOther: test.unreadableOther,
					},
				)
				var output, warnings bytes.Buffer
				err := skills.Update(
					t.Context(),
					manager,
					skills.UpdateOptions{
						Scope: commandio.ScopeOptions{
							Project: test.project,
							Global:  test.global,
						},
						Skills: test.names,
					},
					commandio.IO{
						Out: &output,
						Err: &warnings,
					},
				)
				if test.wantError != "" {
					if err == nil || !strings.Contains(
						err.Error(),
						test.wantError,
					) {
						t.Fatalf(
							"Update = %v, want %q",
							err,
							test.wantError,
						)
					}
					if diff := cmp.Diff(
						before,
						snapshotManagerFS(
							t,
							filesystem,
						),
					); diff != "" {
						t.Fatal(diff)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var summaries []struct {
					Name string `json:"name"`
					Path string `json:"path"`
				}
				if err := json.Unmarshal(
					output.Bytes(),
					&summaries,
				); err != nil {
					t.Fatal(err)
				}
				var gotPaths []string
				for _, summary := range summaries {
					root := "global/"
					if strings.HasPrefix(
						summary.Path,
						projectRoot,
					) && (!test.atHome || summary.Name != "beta") {
						root = "project/"
					}
					gotPaths = append(
						gotPaths,
						root+filepath.Base(filepath.Dir(summary.Path)),
					)
				}
				if diff := cmp.Diff(
					test.wantPaths,
					gotPaths,
				); diff != "" {
					t.Fatal(diff)
				}
				for _, warning := range test.warnings {
					if !strings.Contains(
						warnings.String(),
						warning,
					) {
						t.Fatalf(
							"warnings %q missing %q",
							warnings.String(),
							warning,
						)
					}
				}
				if len(test.warnings) == 0 && warnings.Len() != 0 {
					t.Fatalf(
						"unexpected warnings: %s",
						warnings.String(),
					)
				}
				for _, updated := range test.wantPaths {
					parts := strings.Split(
						updated,
						"/",
					)
					root, filename, hashField := projectRoot, projectLock, "computedHash"
					if parts[0] == "global" {
						root, filename, hashField = globalRoot, globalLock, "skillFolderHash"
					}
					data, err := afero.ReadFile(
						filesystem,
						filename,
					)
					if err != nil {
						t.Fatal(err)
					}
					var lock struct {
						Skills map[string]map[string]any `json:"skills"`
					}
					if err := json.Unmarshal(
						data,
						&lock,
					); err != nil {
						t.Fatal(err)
					}
					if hash, ok := lock.Skills[parts[1]][hashField].(string); !ok || len(hash) != 64 {
						t.Fatalf(
							"updated lock has no folder hash: %s",
							data,
						)
					}
					if parts[1] == "alpha" {
						data, err := afero.ReadFile(
							filesystem,
							filepath.Join(
								root,
								"alpha",
								"SKILL.md",
							),
						)
						if err != nil || string(data) != alphaUpdated {
							t.Fatalf(
								"skill not updated: %s, %v",
								data,
								err,
							)
						}
						if exists, err := afero.Exists(
							filesystem,
							filepath.Join(
								root,
								"alpha",
								"obsolete.txt",
							),
						); err != nil || exists {
							t.Fatal("obsolete file survived update")
						}
					}
				}
			},
		)
	}
}

type updateInventoryFS struct {
	afero.Fs
	unreadableOther bool
}

func (filesystem updateInventoryFS) Stat(filename string) (fs.FileInfo, error) {
	if filesystem.unreadableOther && filepath.Base(filepath.Dir(filename)) == "ignored" {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.Stat(filename)
}

func writeTrackingFixture(t *testing.T, filesystem afero.Fs, filename string, content string) {
	t.Helper()
	if err := filesystem.MkdirAll(
		filepath.Dir(filename),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(
		filesystem,
		filename,
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}
