package skills_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
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

// Independently calculated folder fingerprints for the installed fixtures.
const (
	updateAlphaHash         = "28b5fd9343e6d47f395462fc3d0e9f17779bb14fcbbd6cc0c762ee323b2d2010"
	updateAlphaObsoleteHash = "f407deda4e5076b0e73c1aaa28b73f07a5c6f5a5be69bfc29f2ef158c3aa7ec2"
	updateBetaHash          = "47a0f558e4360d8a9b0f18ea5b6052a5050da4029b0bb150e2987e5529233ffb"
)

func TestUpdate(t *testing.T) {
	// Both inventories contain alpha; names and scope determine which bundles update.
	t.Run(
		"selection",
		func(t *testing.T) {
			for _, test := range []struct {
				name    string
				options skills.UpdateOptions
				want    []string
				warning string
			}{
				// No names means both alpha installations are independent selections.
				{
					name: "all inventories",
					want: []string{
						"project/alpha",
						"global/alpha",
						"global/beta",
					},
				},
				// A named collision is skipped, but the unambiguous beta still updates.
				{
					name: "ambiguous name",
					options: skills.UpdateOptions{
						Skills: []string{
							"alpha",
							"beta",
						},
					},
					want: []string{
						"global/beta",
					},
					warning: "explicit --scope",
				},
				// Explicit scope resolves the collision, and repeated arguments update only once.
				{
					name: "project names",
					options: skills.UpdateOptions{
						Scope: commandio.ScopeOptions{
							Project: true,
						},
						Skills: []string{
							"alpha",
							"alpha",
						},
					},
					want: []string{
						"project/alpha",
					},
				},
				// Global scope must leave the project installation alone.
				{
					name: "global inventory",
					options: skills.UpdateOptions{
						Scope: commandio.ScopeOptions{
							Global: true,
						},
					},
					want: []string{
						"global/alpha",
						"global/beta",
					},
				},
				// A skill in another scope is still missing from the selected scope.
				{
					name: "missing in scope",
					options: skills.UpdateOptions{
						Scope: commandio.ScopeOptions{
							Project: true,
						},
						Skills: []string{
							"beta",
						},
					},
					warning: "does not exist",
				},
			} {
				t.Run(
					test.name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						writeTrackingFixture(
							t,
							f.filesystem,
							filepath.Join(
								f.globalRoot,
								"alpha",
								"SKILL.md",
							),
							alphaSkill,
						)
						f.global.Skills["alpha"] = updateRecord{
							Source:          managerSource,
							SourceType:      "git",
							SourceURL:       managerSource,
							SkillPath:       "alpha/SKILL.md",
							SkillFolderHash: updateAlphaHash,
						}
						f.saveLocks(t)
						before := snapshotManagerFS(
							t,
							f.filesystem,
						)
						paths, err := f.run(
							t,
							test.options,
						)
						if err != nil {
							t.Fatal(err)
						}
						var wantPaths []string
						for _, name := range test.want {
							root, skill, _ := strings.Cut(
								name,
								"/",
							)
							directory := f.projectRoot
							if root == "global" {
								directory = f.globalRoot
							}
							wantPaths = append(
								wantPaths,
								filepath.Join(
									directory,
									skill,
									"SKILL.md",
								),
							)
						}
						if diff := cmp.Diff(
							wantPaths,
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						if test.warning == "" && f.warnings.Len() != 0 || test.warning != "" && !strings.Contains(
							f.warnings.String(),
							test.warning,
						) {
							t.Fatalf(
								"warnings = %q; want %q",
								f.warnings.String(),
								test.warning,
							)
						}
						// Every selected alpha is replaced; every unselected alpha keeps its old bytes.
						for _, root := range []string{
							f.projectRoot,
							f.globalRoot,
						} {
							filename := filepath.Join(
								root,
								"alpha",
								"SKILL.md",
							)
							want := before[filename]
							for _, selected := range paths {
								if selected == filename {
									want = alphaUpdated
								}
							}
							data, err := afero.ReadFile(
								f.filesystem,
								filename,
							)
							if err != nil || string(data) != want {
								t.Fatalf(
									"%s = %q, %v; want %q",
									filename,
									data,
									err,
									want,
								)
							}
						}
					},
				)
			}
		},
	)

	// Untracked and missing selections warn without blocking a tracked sibling.
	t.Run(
		"untracked selections",
		func(t *testing.T) {
			for _, test := range []struct {
				name     string
				names    []string
				warnings []string
			}{
				{
					name: "all installed",
					warnings: []string{
						"not in the lock file",
					},
				},
				{
					name: "named and missing",
					names: []string{
						"missing",
						"alpha",
						"beta",
					},
					warnings: []string{
						"does not exist",
						"not in the lock file",
					},
				},
			} {
				t.Run(
					test.name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						delete(
							f.project.Skills,
							"alpha",
						)
						f.saveLocks(t)
						paths, err := f.run(
							t,
							skills.UpdateOptions{
								Skills: test.names,
							},
						)
						if err != nil {
							t.Fatal(err)
						}
						if diff := cmp.Diff(
							[]string{
								filepath.Join(
									f.globalRoot,
									"beta",
									"SKILL.md",
								),
							},
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						for _, warning := range test.warnings {
							if !strings.Contains(
								f.warnings.String(),
								warning,
							) {
								t.Fatalf(
									"warnings = %q; missing %q",
									f.warnings.String(),
									warning,
								)
							}
						}
						data, err := afero.ReadFile(
							f.filesystem,
							filepath.Join(
								f.projectRoot,
								"alpha",
								"SKILL.md",
							),
						)
						if err != nil || string(data) != alphaSkill {
							t.Fatalf(
								"untracked skill changed: %q, %v",
								data,
								err,
							)
						}
					},
				)
			}
		},
	)

	// Running from HOME shares a bundle directory, but the tracking files remain distinct.
	t.Run(
		"shared directory",
		func(t *testing.T) {
			alpha := updateRecord{
				Source:       managerSource,
				SourceType:   "git",
				SourceURL:    managerSource,
				SkillPath:    "alpha/SKILL.md",
				ComputedHash: updateAlphaObsoleteHash,
			}
			otherAlpha := updateRecord{
				Source:          managerSource,
				SourceType:      "git",
				SourceURL:       managerSource,
				SkillPath:       "other-alpha/SKILL.md",
				SkillFolderHash: updateAlphaObsoleteHash,
			}
			for _, test := range []struct {
				name         string
				projectAlpha *updateRecord
				globalAlpha  *updateRecord
				scope        commandio.ScopeOptions
				want         []string
				warning      string
			}{
				// Missing project tracking must not hide the globally tracked beta.
				{
					name: "global tracking only",
					want: []string{
						"beta",
					},
					warning: "not in the lock file",
				},
				// Disjoint records in the two locks can update their respective bundles.
				{
					name:         "disjoint records",
					projectAlpha: &alpha,
					want: []string{
						"alpha",
						"beta",
					},
					warning: "not in the lock file",
				},
				// Two records for one physical alpha cannot choose competing source contents.
				{
					name:         "conflicting records",
					projectAlpha: &alpha,
					globalAlpha:  &otherAlpha,
					want: []string{
						"beta",
					},
					warning: "one installed bundle",
				},
				// Explicit scope chooses the project record without touching the global record.
				{
					name:         "explicit project",
					projectAlpha: &alpha,
					globalAlpha:  &otherAlpha,
					scope: commandio.ScopeOptions{
						Project: true,
					},
					want: []string{
						"alpha",
					},
					warning: "not in the lock file",
				},
			} {
				t.Run(
					test.name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						t.Chdir(f.home)
						f.projectRoot = f.globalRoot
						f.projectPath = filepath.Join(
							f.home,
							"skills-lock.json",
						)
						f.project.Skills = map[string]updateRecord{}
						if test.projectAlpha != nil {
							f.project.Skills["alpha"] = *test.projectAlpha
						}
						if test.globalAlpha != nil {
							f.global.Skills["alpha"] = *test.globalAlpha
						}
						writeTrackingFixture(
							t,
							f.filesystem,
							filepath.Join(
								f.projectRoot,
								"alpha",
								"SKILL.md",
							),
							alphaSkill,
						)
						writeTrackingFixture(
							t,
							f.filesystem,
							filepath.Join(
								f.projectRoot,
								"alpha",
								"obsolete.txt",
							),
							"obsolete",
						)
						f.source.files["other-alpha/SKILL.md"] = &fstest.MapFile{
							Data: []byte(betaSkill),
						}
						f.saveLocks(t)
						paths, err := f.run(
							t,
							skills.UpdateOptions{
								Scope: test.scope,
							},
						)
						if err != nil {
							t.Fatal(err)
						}
						var wantPaths []string
						for _, name := range test.want {
							wantPaths = append(
								wantPaths,
								filepath.Join(
									f.projectRoot,
									name,
									"SKILL.md",
								),
							)
						}
						if diff := cmp.Diff(
							wantPaths,
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						if !strings.Contains(
							f.warnings.String(),
							test.warning,
						) {
							t.Fatalf(
								"warnings = %q; want %q",
								f.warnings.String(),
								test.warning,
							)
						}
						wantAlpha := alphaSkill
						for _, name := range test.want {
							if name == "alpha" {
								wantAlpha = alphaUpdated
							}
						}
						data, err := afero.ReadFile(
							f.filesystem,
							filepath.Join(
								f.projectRoot,
								"alpha",
								"SKILL.md",
							),
						)
						if err != nil || string(data) != wantAlpha {
							t.Fatalf(
								"shared alpha = %q, %v; want %q",
								data,
								err,
								wantAlpha,
							)
						}
						// Regardless of the chosen scope, the competing global alpha record is preserved.
						global := readUpdateLock(
							t,
							f.filesystem,
							f.globalPath,
						)
						if diff := cmp.Diff(
							f.global.Skills["alpha"],
							global.Skills["alpha"],
						); diff != "" {
							t.Fatal(diff)
						}
					},
				)
			}
		},
	)

	// Unsupported provenance is skipped before acquisition, even with override.
	t.Run(
		"recorded sources",
		func(t *testing.T) {
			for _, test := range []struct {
				name     string
				record   updateRecord
				override bool
				warning  string
			}{
				{
					name: "unsupported type",
					record: updateRecord{
						Source:     managerSource,
						SourceType: "node_modules",
						SkillPath:  "alpha/SKILL.md",
					},
					warning: "unsupported source type",
				},
				{
					name: "unsupported URL",
					record: updateRecord{
						Source:     managerSource,
						SourceType: "git",
						SourceURL:  "git@github.com:owner/repo.git",
						SkillPath:  "alpha/SKILL.md",
					},
					warning: "unsupported recorded source",
				},
				{
					name: "missing URL",
					record: updateRecord{
						Source:     "owner/repo",
						SourceType: "git",
						SkillPath:  "alpha/SKILL.md",
					},
					warning: "unambiguous sourceUrl",
				},
				{
					name: "override does not bypass provenance",
					record: updateRecord{
						Source:     managerSource,
						SourceType: "node_modules",
						SkillPath:  "alpha/SKILL.md",
					},
					override: true,
					warning:  "unsupported source type",
				},
			} {
				t.Run(
					test.name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						f.project.Skills["alpha"] = test.record
						f.saveLocks(t)
						before, err := afero.ReadFile(
							f.filesystem,
							f.projectPath,
						)
						if err != nil {
							t.Fatal(err)
						}
						paths, err := f.run(
							t,
							skills.UpdateOptions{
								Override: test.override,
							},
						)
						if err != nil {
							t.Fatal(err)
						}
						if diff := cmp.Diff(
							[]string{
								filepath.Join(
									f.globalRoot,
									"beta",
									"SKILL.md",
								),
							},
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						if !strings.Contains(
							f.warnings.String(),
							test.warning,
						) {
							t.Fatalf(
								"warnings = %q; want %q",
								f.warnings.String(),
								test.warning,
							)
						}
						after, err := afero.ReadFile(
							f.filesystem,
							f.projectPath,
						)
						if err != nil || !bytes.Equal(
							before,
							after,
						) {
							t.Fatalf(
								"unsupported record changed: %s, %v",
								after,
								err,
							)
						}
					},
				)
			}
		},
	)

	// Edited resources and added files are protected; override replaces the whole bundle.
	t.Run(
		"installed contents",
		func(t *testing.T) {
			for _, test := range []struct {
				name     string
				file     string
				content  string
				override bool
			}{
				{
					name:    "edited resource",
					file:    "obsolete.txt",
					content: "local edit",
				},
				{
					name:    "added dependency",
					file:    "node_modules/local.txt",
					content: "local addition",
				},
				{
					name:    "added Git metadata",
					file:    ".git/local.txt",
					content: "local addition",
				},
				{
					name:     "override edited resource",
					file:     "obsolete.txt",
					content:  "local edit",
					override: true,
				},
				{
					name:     "override added dependency",
					file:     "node_modules/local.txt",
					content:  "local addition",
					override: true,
				},
			} {
				t.Run(
					test.name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						filename := filepath.Join(
							f.projectRoot,
							"alpha",
							filepath.FromSlash(test.file),
						)
						writeTrackingFixture(
							t,
							f.filesystem,
							filename,
							test.content,
						)
						before := snapshotManagerFS(
							t,
							f.filesystem,
						)
						paths, err := f.run(
							t,
							skills.UpdateOptions{
								Scope: commandio.ScopeOptions{
									Project: true,
								},
								Override: test.override,
							},
						)
						if err != nil {
							t.Fatal(err)
						}
						if !test.override {
							if len(paths) != 0 || len(f.source.fetches) != 0 || !strings.Contains(
								f.warnings.String(),
								"--override",
							) {
								t.Fatalf(
									"edited skill was not skipped: paths=%v fetches=%v warnings=%q",
									paths,
									f.source.fetches,
									f.warnings.String(),
								)
							}
							if diff := cmp.Diff(
								before,
								snapshotManagerFS(
									t,
									f.filesystem,
								),
							); diff != "" {
								t.Fatal(diff)
							}
							return
						}
						if diff := cmp.Diff(
							[]string{
								filepath.Join(
									f.projectRoot,
									"alpha",
									"SKILL.md",
								),
							},
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						if len(f.source.fetches) != 1 || f.warnings.Len() != 0 {
							t.Fatalf(
								"override fetches=%v warnings=%q",
								f.source.fetches,
								f.warnings.String(),
							)
						}
						if _, err := f.filesystem.Stat(filename); !errors.Is(
							err,
							fs.ErrNotExist,
						) {
							t.Fatalf(
								"local file survived override: %v",
								err,
							)
						}
						data, err := afero.ReadFile(
							f.filesystem,
							paths[0],
						)
						if err != nil || string(data) != alphaUpdated {
							t.Fatalf(
								"replacement = %q, %v",
								data,
								err,
							)
						}
					},
				)
			}
		},
	)

	// A missing fingerprint cannot establish that project contents are unmodified.
	t.Run(
		"missing project hash",
		func(t *testing.T) {
			f := newUpdateFixture(t)
			record := f.project.Skills["alpha"]
			record.ComputedHash = ""
			f.project.Skills["alpha"] = record
			f.saveLocks(t)
			before := snapshotManagerFS(
				t,
				f.filesystem,
			)
			paths, err := f.run(
				t,
				skills.UpdateOptions{
					Scope: commandio.ScopeOptions{
						Project: true,
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) != 0 || len(f.source.fetches) != 0 || !strings.Contains(
				f.warnings.String(),
				"--override",
			) {
				t.Fatalf(
					"missing hash was not skipped: %v, %v, %q",
					paths,
					f.source.fetches,
					f.warnings.String(),
				)
			}
			if diff := cmp.Diff(
				before,
				snapshotManagerFS(
					t,
					f.filesystem,
				),
			); diff != "" {
				t.Fatal(diff)
			}
		},
	)

	// An incompatible global fingerprint needs override before CPE can replace it.
	t.Run(
		"incompatible global hash",
		func(t *testing.T) {
			for _, override := range []bool{
				false,
				true,
			} {
				name := "skip"
				if override {
					name = "override"
				}
				t.Run(
					name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						record := f.global.Skills["beta"]
						record.SkillFolderHash = "70993c44c50665a842d44706f3daf42d0530412f"
						f.global.Skills["beta"] = record
						f.saveLocks(t)
						before := snapshotManagerFS(
							t,
							f.filesystem,
						)
						paths, err := f.run(
							t,
							skills.UpdateOptions{
								Scope: commandio.ScopeOptions{
									Global: true,
								},
								Override: override,
							},
						)
						if err != nil {
							t.Fatal(err)
						}
						if !override {
							if len(paths) != 0 || len(f.source.fetches) != 0 || !strings.Contains(
								f.warnings.String(),
								"incompatible tracking",
							) {
								t.Fatalf(
									"incompatible hash was not skipped: %v, %v, %q",
									paths,
									f.source.fetches,
									f.warnings.String(),
								)
							}
							if diff := cmp.Diff(
								before,
								snapshotManagerFS(
									t,
									f.filesystem,
								),
							); diff != "" {
								t.Fatal(diff)
							}
							return
						}
						if diff := cmp.Diff(
							[]string{
								filepath.Join(
									f.globalRoot,
									"beta",
									"SKILL.md",
								),
							},
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						lock := readUpdateLock(
							t,
							f.filesystem,
							f.globalPath,
						)
						if lock.Skills["beta"].SkillFolderHash != updateBetaHash || len(f.source.fetches) != 1 || f.warnings.Len() != 0 {
							t.Fatalf(
								"override did not adopt CPE tracking: %+v; fetches=%v warnings=%q",
								lock,
								f.source.fetches,
								f.warnings.String(),
							)
						}
					},
				)
			}
		},
	)

	// A resource read failure is a warning by default; override does not read that resource.
	t.Run(
		"unreadable installed resource",
		func(t *testing.T) {
			for _, override := range []bool{
				false,
				true,
			} {
				name := "skip"
				if override {
					name = "override"
				}
				t.Run(
					name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						base := f.filesystem
						before := snapshotManagerFS(
							t,
							base,
						)
						f.filesystem = updateUnreadableFS{
							Fs: base,
							filename: filepath.Join(
								f.projectRoot,
								"alpha",
								"obsolete.txt",
							),
						}
						paths, err := f.run(
							t,
							skills.UpdateOptions{
								Scope: commandio.ScopeOptions{
									Project: true,
								},
								Override: override,
							},
						)
						if err != nil {
							t.Fatal(err)
						}
						if !override {
							if len(paths) != 0 || len(f.source.fetches) != 0 || !strings.Contains(
								f.warnings.String(),
								"cannot verify installed contents",
							) {
								t.Fatalf(
									"unreadable bundle was not skipped: %v, %v, %q",
									paths,
									f.source.fetches,
									f.warnings.String(),
								)
							}
							if diff := cmp.Diff(
								before,
								snapshotManagerFS(
									t,
									base,
								),
							); diff != "" {
								t.Fatal(diff)
							}
							return
						}
						if diff := cmp.Diff(
							[]string{
								filepath.Join(
									f.projectRoot,
									"alpha",
									"SKILL.md",
								),
							},
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						if len(f.source.fetches) != 1 || f.warnings.Len() != 0 {
							t.Fatalf(
								"override fetches=%v warnings=%q",
								f.source.fetches,
								f.warnings.String(),
							)
						}
					},
				)
			}
		},
	)

	// Only eligible sources are cloned, once each; payloads survive checkout cleanup.
	t.Run(
		"source acquisition",
		func(t *testing.T) {
			const otherSource = "https://example.com/other.git"
			for _, test := range []struct {
				name          string
				betaSource    string
				alphaResource string
				wantSources   []string
				wantNames     []string
				warning       string
			}{
				{
					name:          "shared source",
					betaSource:    managerSource,
					alphaResource: "obsolete",
					wantSources: []string{
						managerSource,
					},
					wantNames: []string{
						"alpha",
						"beta",
					},
				},
				{
					name:          "different sources",
					betaSource:    otherSource,
					alphaResource: "obsolete",
					wantSources: []string{
						managerSource,
						otherSource,
					},
					wantNames: []string{
						"alpha",
						"beta",
					},
				},
				{
					name:          "shared source with edited skill",
					betaSource:    managerSource,
					alphaResource: "local edit",
					wantSources: []string{
						managerSource,
					},
					wantNames: []string{
						"beta",
					},
					warning: "installed contents do not match",
				},
				{
					name:          "skip edited skills source",
					betaSource:    otherSource,
					alphaResource: "local edit",
					wantSources: []string{
						otherSource,
					},
					wantNames: []string{
						"beta",
					},
					warning: "installed contents do not match",
				},
			} {
				t.Run(
					test.name,
					func(t *testing.T) {
						f := newUpdateFixture(t)
						record := f.global.Skills["beta"]
						record.Source, record.SourceURL = test.betaSource, test.betaSource
						f.global.Skills["beta"] = record
						f.saveLocks(t)
						writeTrackingFixture(
							t,
							f.filesystem,
							filepath.Join(
								f.projectRoot,
								"alpha",
								"obsolete.txt",
							),
							test.alphaResource,
						)
						before := snapshotManagerFS(
							t,
							f.filesystem,
						)
						paths, err := f.run(
							t,
							skills.UpdateOptions{},
						)
						if err != nil {
							t.Fatal(err)
						}
						if diff := cmp.Diff(
							test.wantSources,
							f.source.fetches,
						); diff != "" {
							t.Fatal(diff)
						}
						if f.source.cleanups != len(test.wantSources) {
							t.Fatalf(
								"cleanups = %d; want %d",
								f.source.cleanups,
								len(test.wantSources),
							)
						}
						var wantPaths []string
						for _, name := range test.wantNames {
							root := f.projectRoot
							if name == "beta" {
								root = f.globalRoot
							}
							wantPaths = append(
								wantPaths,
								filepath.Join(
									root,
									name,
									"SKILL.md",
								),
							)
						}
						if diff := cmp.Diff(
							wantPaths,
							paths,
						); diff != "" {
							t.Fatal(diff)
						}
						if test.warning == "" && f.warnings.Len() != 0 || test.warning != "" && !strings.Contains(
							f.warnings.String(),
							test.warning,
						) {
							t.Fatalf(
								"warnings = %q; want %q",
								f.warnings.String(),
								test.warning,
							)
						}
						if test.warning != "" {
							for _, filename := range []string{
								f.projectPath,
								filepath.Join(
									f.projectRoot,
									"alpha",
									"obsolete.txt",
								),
							} {
								data, err := afero.ReadFile(
									f.filesystem,
									filename,
								)
								if err != nil || string(data) != before[filename] {
									t.Fatalf(
										"skipped installation changed %s: %q, %v",
										filename,
										data,
										err,
									)
								}
							}
							return
						}
						// Successful publication replaces resources and writes the new project fingerprint.
						data, err := afero.ReadFile(
							f.filesystem,
							filepath.Join(
								f.projectRoot,
								"alpha",
								"SKILL.md",
							),
						)
						if err != nil || string(data) != alphaUpdated {
							t.Fatalf(
								"replacement = %q, %v",
								data,
								err,
							)
						}
						if _, err := f.filesystem.Stat(filepath.Join(
							f.projectRoot,
							"alpha",
							"obsolete.txt",
						)); !errors.Is(
							err,
							fs.ErrNotExist,
						) {
							t.Fatalf(
								"obsolete resource survived: %v",
								err,
							)
						}
						lock := readUpdateLock(
							t,
							f.filesystem,
							f.projectPath,
						)
						if len(lock.Skills["alpha"].ComputedHash) != 64 {
							t.Fatalf(
								"missing updated fingerprint: %+v",
								lock,
							)
						}
					},
				)
			}
		},
	)

	// Named selection must not stat an unrelated damaged definition in either scope.
	t.Run(
		"unselected definitions",
		func(t *testing.T) {
			f := newUpdateFixture(t)
			blocked := map[string]bool{}
			for _, root := range []string{
				f.projectRoot,
				f.globalRoot,
			} {
				filename := filepath.Join(
					root,
					"ignored",
					"SKILL.md",
				)
				writeTrackingFixture(
					t,
					f.filesystem,
					filename,
					"invalid definition",
				)
				blocked[filename] = true
			}
			f.filesystem = updateUnselectedFS{
				Fs:      f.filesystem,
				blocked: blocked,
			}
			paths, err := f.run(
				t,
				skills.UpdateOptions{
					Skills: []string{
						"alpha",
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(
				[]string{
					filepath.Join(
						f.projectRoot,
						"alpha",
						"SKILL.md",
					),
				},
				paths,
			); diff != "" {
				t.Fatal(diff)
			}
			if f.warnings.Len() != 0 {
				t.Fatalf(
					"unexpected warnings: %s",
					f.warnings.String(),
				)
			}
		},
	)

	// Invalid tracking prevents fetching and leaves both inventories untouched.
	t.Run(
		"corrupt tracking",
		func(t *testing.T) {
			f := newUpdateFixture(t)
			writeTrackingFixture(
				t,
				f.filesystem,
				f.projectPath,
				"invalid JSON",
			)
			before := snapshotManagerFS(
				t,
				f.filesystem,
			)
			paths, err := f.run(
				t,
				skills.UpdateOptions{},
			)
			if err == nil || !strings.Contains(
				err.Error(),
				"lock",
			) || len(paths) != 0 || len(f.source.fetches) != 0 {
				t.Fatalf(
					"paths=%v fetches=%v error=%v",
					paths,
					f.source.fetches,
					err,
				)
			}
			if diff := cmp.Diff(
				before,
				snapshotManagerFS(
					t,
					f.filesystem,
				),
			); diff != "" {
				t.Fatal(diff)
			}
		},
	)

	// A failed checkout cleanup stops publication even after its payload was read.
	t.Run(
		"source cleanup failure",
		func(t *testing.T) {
			f := newUpdateFixture(t)
			f.source.cleanupErr = errors.New("cleanup failed")
			before := snapshotManagerFS(
				t,
				f.filesystem,
			)
			paths, err := f.run(
				t,
				skills.UpdateOptions{},
			)
			if !errors.Is(
				err,
				f.source.cleanupErr,
			) || len(paths) != 0 || len(f.source.fetches) != 1 || f.source.cleanups != 1 {
				t.Fatalf(
					"paths=%v fetches=%v cleanups=%d error=%v",
					paths,
					f.source.fetches,
					f.source.cleanups,
					err,
				)
			}
			if diff := cmp.Diff(
				before,
				snapshotManagerFS(
					t,
					f.filesystem,
				),
			); diff != "" {
				t.Fatal(diff)
			}
		},
	)

	// A bundle publication failure must leave its tracking and old contents intact.
	t.Run(
		"bundle publication failure",
		func(t *testing.T) {
			f := newUpdateFixture(t)
			before := snapshotManagerFS(
				t,
				f.filesystem,
			)
			f.filesystem = managerFaultFS{
				Fs:    f.filesystem,
				fault: managerPublishFailure,
			}
			paths, err := f.run(
				t,
				skills.UpdateOptions{
					Scope: commandio.ScopeOptions{
						Project: true,
					},
				},
			)
			if !errors.Is(
				err,
				fs.ErrPermission,
			) || len(paths) != 0 {
				t.Fatalf(
					"paths=%v error=%v",
					paths,
					err,
				)
			}
			if diff := cmp.Diff(
				before,
				snapshotManagerFS(
					t,
					f.filesystem,
				),
			); diff != "" {
				t.Fatal(diff)
			}
		},
	)

	// Failure in global scope still reports the already committed project update.
	t.Run(
		"partial results",
		func(t *testing.T) {
			f := newUpdateFixture(t)
			delete(
				f.source.files,
				"beta/SKILL.md",
			)
			before := snapshotManagerFS(
				t,
				f.filesystem,
			)
			paths, err := f.run(
				t,
				skills.UpdateOptions{},
			)
			if err == nil || !strings.Contains(
				err.Error(),
				"some skills updates committed",
			) {
				t.Fatalf(
					"error = %v",
					err,
				)
			}
			if diff := cmp.Diff(
				[]string{
					filepath.Join(
						f.projectRoot,
						"alpha",
						"SKILL.md",
					),
				},
				paths,
			); diff != "" {
				t.Fatal(diff)
			}
			data, err := afero.ReadFile(
				f.filesystem,
				paths[0],
			)
			if err != nil || string(data) != alphaUpdated {
				t.Fatalf(
					"project update missing: %q, %v",
					data,
					err,
				)
			}
			for _, filename := range []string{
				f.globalPath,
				filepath.Join(
					f.globalRoot,
					"beta",
					"SKILL.md",
				),
			} {
				data, err := afero.ReadFile(
					f.filesystem,
					filename,
				)
				if err != nil || string(data) != before[filename] {
					t.Fatalf(
						"failed global scope changed %s: %q, %v",
						filename,
						data,
						err,
					)
				}
			}
			if len(f.source.fetches) != 1 || f.source.cleanups != 1 {
				t.Fatalf(
					"fetches=%v cleanups=%d",
					f.source.fetches,
					f.source.cleanups,
				)
			}
		},
	)
}

// The fixture starts with project alpha (including an obsolete resource), global
// beta, and valid tracking. Tests change concrete files or records before running.
type updateFixture struct {
	filesystem  afero.Fs
	home        string
	projectRoot string
	globalRoot  string
	projectPath string
	globalPath  string
	project     updateLock
	global      updateLock
	source      *updateSourceClient
	warnings    bytes.Buffer
}

type updateRecord struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	SkillPath       string `json:"skillPath"`
	ComputedHash    string `json:"computedHash,omitempty"`
	SkillFolderHash string `json:"skillFolderHash,omitempty"`
}

type updateLock struct {
	Version int                     `json:"version"`
	Skills  map[string]updateRecord `json:"skills"`
}

func newUpdateFixture(t *testing.T) *updateFixture {
	t.Helper()
	project, home := t.TempDir(), t.TempDir()
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
	f := &updateFixture{
		filesystem: afero.NewMemMapFs(),
		home:       home,
		projectRoot: filepath.Join(
			project,
			".agents",
			"skills",
		),
		globalRoot: filepath.Join(
			home,
			".agents",
			"skills",
		),
		projectPath: filepath.Join(
			project,
			"skills-lock.json",
		),
		globalPath: filepath.Join(
			home,
			".agents",
			".skill-lock.json",
		),
		project: updateLock{
			Version: 1,
			Skills: map[string]updateRecord{
				"alpha": {
					Source:       managerSource,
					SourceType:   "git",
					SourceURL:    managerSource,
					SkillPath:    "alpha/SKILL.md",
					ComputedHash: updateAlphaObsoleteHash,
				},
			},
		},
		global: updateLock{
			Version: 3,
			Skills: map[string]updateRecord{
				"beta": {
					Source:          managerSource,
					SourceType:      "git",
					SourceURL:       managerSource,
					SkillPath:       "beta/SKILL.md",
					SkillFolderHash: updateBetaHash,
				},
			},
		},
		source: &updateSourceClient{
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
		},
	}
	for filename, content := range map[string]string{
		filepath.Join(
			f.projectRoot,
			"alpha",
			"SKILL.md",
		): alphaSkill,
		filepath.Join(
			f.projectRoot,
			"alpha",
			"obsolete.txt",
		): "obsolete",
		filepath.Join(
			f.globalRoot,
			"beta",
			"SKILL.md",
		): betaSkill,
	} {
		writeTrackingFixture(
			t,
			f.filesystem,
			filename,
			content,
		)
	}
	f.saveLocks(t)
	return f
}

func (f *updateFixture) saveLocks(t *testing.T) {
	t.Helper()
	for filename, lock := range map[string]updateLock{
		f.projectPath: f.project,
		f.globalPath:  f.global,
	} {
		data, err := json.Marshal(lock)
		if err != nil {
			t.Fatal(err)
		}
		writeTrackingFixture(
			t,
			f.filesystem,
			filename,
			string(data),
		)
	}
}

// run invokes the package command and decodes its reported paths, including any
// partial results. Success, warnings, and filesystem effects are asserted by tests.
func (f *updateFixture) run(t *testing.T, options skills.UpdateOptions) ([]string, error) {
	t.Helper()
	var output bytes.Buffer
	err := skills.Update(
		t.Context(),
		skills.NewManager(
			f.source,
			f.filesystem,
		),
		options,
		commandio.IO{
			Out: &output,
			Err: &f.warnings,
		},
	)
	var summaries []struct {
		Path string `json:"path"`
	}
	if output.Len() != 0 {
		if decodeErr := json.Unmarshal(
			output.Bytes(),
			&summaries,
		); decodeErr != nil {
			t.Fatal(decodeErr)
		}
	}
	var paths []string
	for _, summary := range summaries {
		paths = append(
			paths,
			summary.Path,
		)
	}
	return paths, err
}

func readUpdateLock(t *testing.T, filesystem afero.Fs, filename string) updateLock {
	t.Helper()
	data, err := afero.ReadFile(
		filesystem,
		filename,
	)
	if err != nil {
		t.Fatal(err)
	}
	var lock updateLock
	if err := json.Unmarshal(
		data,
		&lock,
	); err != nil {
		t.Fatal(err)
	}
	return lock
}

type updateSourceClient struct {
	files      fstest.MapFS
	fetches    []string
	cleanups   int
	cleanupErr error
}

func (client *updateSourceClient) Fetch(_ context.Context, locator string) (source.Checkout, error) {
	client.fetches = append(
		client.fetches,
		locator,
	)
	files := maps.Clone(client.files)
	return source.Checkout{
		FS: files,
		Cleanup: func() error {
			// A closed checkout is no longer usable; cached payloads must own their bytes.
			clear(files)
			client.cleanups++
			return client.cleanupErr
		},
	}, nil
}

type updateUnreadableFS struct {
	afero.Fs
	filename string
}

func (filesystem updateUnreadableFS) Open(filename string) (afero.File, error) {
	if filename == filesystem.filename {
		return nil, fs.ErrPermission
	}
	return filesystem.Fs.Open(filename)
}

type updateUnselectedFS struct {
	afero.Fs
	blocked map[string]bool
}

func (filesystem updateUnselectedFS) Stat(filename string) (fs.FileInfo, error) {
	if filesystem.blocked[filename] {
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
