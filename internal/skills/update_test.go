package skills_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
	"github.com/spachava753/cpe/internal/source"
)

func TestUpdate(t *testing.T) {
	// Independently calculated full-folder fingerprints for these fixtures.
	const (
		alphaHash         = "28b5fd9343e6d47f395462fc3d0e9f17779bb14fcbbd6cc0c762ee323b2d2010"
		alphaObsoleteHash = "f407deda4e5076b0e73c1aaa28b73f07a5c6f5a5be69bfc29f2ef158c3aa7ec2"
		betaHash          = "47a0f558e4360d8a9b0f18ea5b6052a5050da4029b0bb150e2987e5529233ffb"
		otherSource       = "https://example.com/other.git"
	)
	for _, test := range []struct {
		name                string
		names               []string
		project             bool
		global              bool
		atHome              bool
		native              bool
		overlapping         bool
		duplicate           bool
		untracked           bool
		unsupported         bool
		unsupportedLocator  bool
		missingURL          bool
		unreadableOther     bool
		corrupt             bool
		override            bool
		localEdit           bool
		localDirectory      string
		missingHash         bool
		legacyHash          bool
		github              bool
		unreadableInstalled bool
		consumeSource       bool
		differentSource     bool
		missingSourceSkill  bool
		cleanupFailure      bool
		wantCommitted       bool
		wantFetches         *int
		fault               managerFSFault
		wantPaths           []string
		warnings            []string
		wantError           string
	}{
		{
			name:        "all scopes include both inventories and share one source acquisition",
			wantFetches: new(1),
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
			name:        "shared-root tracked collisions skip while disjoint updates continue",
			atHome:      true,
			duplicate:   true,
			overlapping: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"one installed bundle",
				"explicit --scope",
			},
		},
		{
			name:        "native shared-root tracked collisions skip while disjoint updates continue",
			atHome:      true,
			native:      true,
			duplicate:   true,
			overlapping: true,
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"one installed bundle",
				"explicit --scope",
			},
		},
		{
			name:        "explicit scope resolves a shared-root tracked collision",
			atHome:      true,
			project:     true,
			duplicate:   true,
			overlapping: true,
			wantPaths: []string{
				"project/alpha",
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
			name:        "local edits are skipped before any clone",
			project:     true,
			localEdit:   true,
			wantFetches: new(0),
			warnings: []string{
				"installed contents do not match",
				"--override",
			},
		},
		{
			name:           "locally added dependency files require override",
			project:        true,
			localDirectory: "node_modules",
			wantFetches:    new(0),
			warnings: []string{
				"installed contents do not match",
			},
		},
		{
			name:           "locally added Git metadata requires override",
			project:        true,
			localDirectory: ".git",
			wantFetches:    new(0),
			warnings: []string{
				"installed contents do not match",
			},
		},
		{
			name:           "override replaces locally added dependency files",
			project:        true,
			localDirectory: "node_modules",
			override:       true,
			wantFetches:    new(1),
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:        "override replaces local edits",
			project:     true,
			localEdit:   true,
			override:    true,
			wantFetches: new(1),
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:        "missing hash cannot establish unmodified installed content",
			project:     true,
			missingHash: true,
			wantFetches: new(0),
			warnings: []string{
				"installed contents do not match",
				"--override",
			},
		},
		{
			name:        "incompatible hash requires explicit override",
			project:     true,
			legacyHash:  true,
			wantFetches: new(0),
			warnings: []string{
				"incompatible tracking",
				"--override",
			},
		},
		{
			name:        "override adopts an incompatible project hash",
			project:     true,
			legacyHash:  true,
			override:    true,
			wantFetches: new(1),
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:        "global GitHub tree ID cannot verify installed content",
			global:      true,
			github:      true,
			legacyHash:  true,
			wantFetches: new(0),
			warnings: []string{
				"incompatible tracking",
				"--override",
			},
		},
		{
			name:        "override adopts a global GitHub folder fingerprint",
			global:      true,
			github:      true,
			legacyHash:  true,
			override:    true,
			wantFetches: new(1),
			wantPaths: []string{
				"global/beta",
			},
		},
		{
			name:        "eligible sibling proceeds while edited skill is skipped",
			localEdit:   true,
			wantFetches: new(1),
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"installed contents do not match",
			},
		},
		{
			name:            "skipped source is not acquired while a different eligible source proceeds",
			localEdit:       true,
			differentSource: true,
			wantFetches:     new(1),
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"installed contents do not match",
			},
		},
		{
			name:            "different eligible sources each get one acquisition",
			differentSource: true,
			wantFetches:     new(2),
			wantPaths: []string{
				"project/alpha",
				"global/beta",
			},
		},
		{
			name:                "unverifiable installed resource warns without cloning",
			project:             true,
			unreadableInstalled: true,
			wantFetches:         new(0),
			warnings: []string{
				"cannot verify installed contents",
				"--override",
			},
		},
		{
			name:                "override does not need to read installed resources",
			project:             true,
			unreadableInstalled: true,
			override:            true,
			wantFetches:         new(1),
			wantPaths: []string{
				"project/alpha",
			},
		},
		{
			name:          "shared snapshots remain usable after source cleanup consumes the checkout",
			consumeSource: true,
			wantFetches:   new(1),
			wantPaths: []string{
				"project/alpha",
				"global/beta",
			},
		},
		{
			name:        "override does not make unsupported provenance eligible",
			unsupported: true,
			override:    true,
			wantFetches: new(1),
			wantPaths: []string{
				"global/beta",
			},
			warnings: []string{
				"unsupported",
			},
		},
		{
			name:           "shared source cleanup failure prevents publication",
			cleanupFailure: true,
			wantFetches:    new(1),
			wantError:      "cleanup failed",
		},
		{
			name:               "later scope source error still reports earlier committed updates",
			missingSourceSkill: true,
			wantCommitted:      true,
			wantFetches:        new(1),
			wantPaths: []string{
				"project/alpha",
			},
			wantError: "some skills updates committed",
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
				if test.native {
					filesystem = afero.NewBasePathFs(
						afero.NewOsFs(),
						t.TempDir(),
					)
				}
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
						"computedHash": alphaObsoleteHash,
						"extra":        "preserved",
					}
				}
				if test.missingHash {
					delete(
						projectRecords["alpha"].(map[string]any),
						"computedHash",
					)
				}
				if test.legacyHash && !test.global {
					projectRecords["alpha"].(map[string]any)["computedHash"] = "old"
				}
				if test.localEdit {
					writeTrackingFixture(
						t,
						filesystem,
						filepath.Join(
							projectRoot,
							"alpha",
							"obsolete.txt",
						),
						"local edit",
					)
				}
				if test.localDirectory != "" {
					writeTrackingFixture(
						t,
						filesystem,
						filepath.Join(
							projectRoot,
							"alpha",
							test.localDirectory,
							"local.txt",
						),
						"local addition",
					)
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
						"skillFolderHash": betaHash,
						"installedAt":     "2026-01-01T00:00:00.000Z",
					},
				}
				if test.differentSource {
					globalRecords["beta"].(map[string]any)["sourceUrl"] = otherSource
					globalRecords["beta"].(map[string]any)["source"] = otherSource
				}
				if test.github {
					globalRecords["beta"].(map[string]any)["sourceType"] = "github"
				}
				if test.legacyHash && test.global {
					globalRecords["beta"].(map[string]any)["skillFolderHash"] = "70993c44c50665a842d44706f3daf42d0530412f"
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
						"skillFolderHash": alphaHash,
					}
					if test.atHome {
						globalRecords["alpha"].(map[string]any)["skillFolderHash"] = alphaObsoleteHash
					}
				}
				if test.overlapping {
					globalRecords["alpha"].(map[string]any)["skillPath"] = "global-alpha/SKILL.md"
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
				remote := fstest.MapFS{
					"alpha/SKILL.md": {
						Data: []byte(alphaUpdated),
					},
					"beta/SKILL.md": {
						Data: []byte(betaSkill),
					},
					"global-alpha/SKILL.md": {
						Data: []byte(strings.Replace(
							alphaUpdated,
							"Updated alpha",
							"Different global payload",
							1,
						)),
					},
				}
				if test.missingSourceSkill {
					delete(
						remote,
						"beta/SKILL.md",
					)
				}
				var fetches []string
				cleanups := 0
				client := updateSourceClient{
					files:   remote,
					fetches: &fetches,
					cleanup: func() error {
						cleanups++
						if test.consumeSource {
							clear(remote)
						}
						if test.cleanupFailure {
							return errors.New("cleanup failed")
						}
						return nil
					},
				}
				manager := skills.NewManager(
					client,
					updateInventoryFS{
						Fs: managerFaultFS{
							Fs:    filesystem,
							fault: test.fault,
						},
						unreadableOther:     test.unreadableOther,
						unreadableInstalled: test.unreadableInstalled,
						resource: filepath.Join(
							projectRoot,
							"alpha",
							"obsolete.txt",
						),
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
						Skills:   test.names,
						Override: test.override,
					},
					commandio.IO{
						Out: &output,
						Err: &warnings,
					},
				)
				if test.wantFetches != nil && len(fetches) != *test.wantFetches {
					t.Fatalf(
						"source acquisitions = %v; want %d",
						fetches,
						*test.wantFetches,
					)
				}
				if cleanups != len(fetches) {
					t.Fatalf(
						"source cleanup count = %d; acquisitions = %v",
						cleanups,
						fetches,
					)
				}
				for index, locator := range fetches {
					for _, earlier := range fetches[:index] {
						if locator == earlier {
							t.Fatalf(
								"source acquired more than once: %v",
								fetches,
							)
						}
					}
				}
				if test.localEdit && test.differentSource && (len(fetches) != 1 || fetches[0] != otherSource) {
					t.Fatalf(
						"skipped source was acquired: %v",
						fetches,
					)
				}
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
					if !test.wantCommitted {
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
				} else if err != nil {
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
				if (test.localEdit || test.localDirectory != "" || test.missingHash || test.legacyHash) && !test.override && !test.global {
					data, err := afero.ReadFile(
						filesystem,
						projectLock,
					)
					if err != nil || string(data) != before[projectLock] {
						t.Fatal("skipped project lock was modified")
					}
					data, err = afero.ReadFile(
						filesystem,
						filepath.Join(
							projectRoot,
							"alpha",
							"obsolete.txt",
						),
					)
					want := "obsolete"
					if test.localEdit {
						want = "local edit"
					}
					if err != nil || string(data) != want {
						t.Fatalf(
							"skipped contents = %q, %v; want %q",
							data,
							err,
							want,
						)
					}
				}
				if test.overlapping && !test.project {
					for _, filename := range []string{
						projectLock,
						filepath.Join(
							projectRoot,
							"alpha",
							"SKILL.md",
						),
						filepath.Join(
							projectRoot,
							"alpha",
							"obsolete.txt",
						),
					} {
						data, err := afero.ReadFile(
							filesystem,
							filename,
						)
						if err != nil || string(data) != before[filename] {
							t.Fatalf(
								"colliding update changed %s: %s, %v",
								filename,
								data,
								err,
							)
						}
					}
					data, err := afero.ReadFile(
						filesystem,
						globalLock,
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
					if lock.Skills["alpha"]["skillFolderHash"] != alphaObsoleteHash {
						t.Fatalf(
							"colliding global record was changed: %s",
							data,
						)
					}
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

type updateSourceClient struct {
	files   fs.FS
	fetches *[]string
	cleanup func() error
}

func (client updateSourceClient) Fetch(_ context.Context, locator string) (source.Checkout, error) {
	*client.fetches = append(
		*client.fetches,
		locator,
	)
	return source.Checkout{
		FS:      client.files,
		Cleanup: client.cleanup,
	}, nil
}

type updateInventoryFS struct {
	afero.Fs
	unreadableOther     bool
	unreadableInstalled bool
	resource            string
}

func (filesystem updateInventoryFS) Stat(filename string) (fs.FileInfo, error) {
	if filesystem.unreadableOther && filepath.Base(filepath.Dir(filename)) == "ignored" {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.Stat(filename)
}

func (filesystem updateInventoryFS) Open(filename string) (afero.File, error) {
	if filesystem.unreadableInstalled && filename == filesystem.resource {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.Open(filename)
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
