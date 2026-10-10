package skills_test

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

func TestSkillTracking(t *testing.T) {
	for _, test := range []struct {
		name           string
		format         skills.LockFormat
		corrupt        bool
		publishFailure bool
		concurrent     bool
	}{
		{
			name:   "project install and remove preserve unrelated records",
			format: skills.ProjectLock,
		},
		{
			name:   "global install and remove preserve preferences",
			format: skills.GlobalLock,
		},
		{
			name:    "corrupt lock prevents install",
			format:  skills.ProjectLock,
			corrupt: true,
		},
		{
			name:       "concurrent lock record and preference changes are not overwritten",
			format:     skills.ProjectLock,
			concurrent: true,
		},
		{
			name:           "lock publication failure rolls back bundles",
			format:         skills.ProjectLock,
			publishFailure: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				filename := filepath.FromSlash("/project/skills-lock.json")
				directory := filepath.FromSlash("/project/.agents/skills")
				version := 1
				if test.format == skills.GlobalLock {
					version = 3
				}
				initial, err := json.Marshal(map[string]any{
					"version": version,
					"skills": map[string]any{
						"foreign": map[string]any{
							"sourceType": "well-known",
							"opaque":     true,
						},
					},
					"lastSelectedAgents": []string{
						"claude-code",
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if test.corrupt {
					initial = []byte("conflict markers")
				}
				writeTrackingFixture(
					t,
					filesystem,
					filename,
					string(initial),
				)
				before := snapshotManagerFS(
					t,
					filesystem,
				)
				var cleanup func() error
				if test.concurrent {
					external := strings.Replace(
						string(initial),
						`"skills":{`,
						`"skills":{"concurrent":{"sourceType":"local"},`,
						1,
					)
					external = strings.Replace(
						external,
						`"lastSelectedAgents":["claude-code"]`,
						`"lastSelectedAgents":["codex"]`,
						1,
					)
					before[filename] = external
					cleanup = func() error {
						return afero.WriteFile(
							filesystem,
							filename,
							[]byte(external),
							0o600,
						)
					}
				}
				manager := skills.NewManager(
					managerSourceClient{
						cleanup: cleanup,
						files: fstest.MapFS{
							"nested/alpha/SKILL.md": {
								Data: []byte(alphaSkill),
							},
							"nested/alpha/resource.txt": {
								Data: []byte("resource"),
							},
						},
					},
					trackingFaultFS{
						Fs:       filesystem,
						filename: filename,
						fail:     test.publishFailure,
					},
				)
				lock := &skills.LockFile{
					Path:   filename,
					Format: test.format,
				}
				installed, err := manager.Install(
					t.Context(),
					skills.InstallRequest{
						Source:    managerSource,
						Directory: directory,
						Lock:      lock,
					},
				)
				if test.corrupt || test.publishFailure || test.concurrent {
					if err == nil || len(installed) != 0 {
						t.Fatalf(
							"Install = %v, %v; want uncommitted error",
							installed,
							err,
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
				data, err := afero.ReadFile(
					filesystem,
					filename,
				)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Skills map[string]map[string]any `json:"skills"`
					Agents []string                  `json:"lastSelectedAgents"`
				}
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				alpha := document.Skills["alpha"]
				if alpha["skillPath"] != "nested/alpha/SKILL.md" || alpha["source"] != managerSource || alpha["sourceType"] != "git" {
					t.Fatalf(
						"incorrect provenance: %s",
						data,
					)
				}
				if _, err := manager.Remove(
					t.Context(),
					skills.RemoveRequest{
						Directory: directory,
						Skills: []string{
							"alpha",
						},
						Lock: lock,
					},
				); err != nil {
					t.Fatal(err)
				}
				data, err = afero.ReadFile(
					filesystem,
					filename,
				)
				if err != nil {
					t.Fatal(err)
				}
				var want, got any
				if err := json.Unmarshal(
					initial,
					&want,
				); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(
					data,
					&got,
				); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(
					want,
					got,
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}

type trackingFaultFS struct {
	afero.Fs
	filename string
	fail     bool
}

func (filesystem trackingFaultFS) Rename(oldname string, newname string) error {
	if filesystem.fail && newname == filesystem.filename && strings.HasPrefix(
		filepath.Base(oldname),
		".cpe-file-",
	) {
		return fs.ErrPermission
	}
	return filesystem.Fs.Rename(
		oldname,
		newname,
	)
}
