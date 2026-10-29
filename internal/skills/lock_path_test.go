package skills_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
	"github.com/spachava753/cpe/internal/skills"
)

func TestSkillLockPathSafety(t *testing.T) {
	for _, test := range []struct {
		name string
		xdg  bool
	}{
		{
			name: "explicit manager lock path",
		},
		{
			name: "raw XDG state root",
			xdg:  true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				root := t.TempDir()
				safe := filepath.Join(
					root,
					"safe",
				)
				foreign := filepath.Join(
					root,
					"foreign",
				)
				for _, directory := range []string{
					filepath.Join(
						safe,
						"outside",
					),
					filepath.Join(
						foreign,
						"child",
					),
					filepath.Join(
						foreign,
						"outside",
					),
				} {
					if err := os.MkdirAll(
						directory,
						0o700,
					); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(
					filepath.Join(
						foreign,
						"child",
					),
					filepath.Join(
						safe,
						"link",
					),
				); err != nil {
					t.Skipf(
						"symlink unavailable: %v",
						err,
					)
				}
				const initial = `{"version":1,"skills":{"foreign":{"opaque":true}}}`
				filename := filepath.Join(
					foreign,
					"outside",
					"skills-lock.json",
				)
				if err := os.WriteFile(
					filename,
					[]byte(initial),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				manager := skills.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							"alpha/SKILL.md": {
								Data: []byte(alphaSkill),
							},
						},
					},
					afero.NewOsFs(),
				)
				unsafeRoot := safe + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "outside"
				var err error
				if test.xdg {
					t.Setenv(
						"HOME",
						safe,
					)
					t.Setenv(
						"USERPROFILE",
						safe,
					)
					t.Setenv(
						"XDG_STATE_HOME",
						unsafeRoot,
					)
					var output, warnings bytes.Buffer
					err = skills.Install(
						t.Context(),
						manager,
						skills.InstallOptions{
							Scope: commandio.ScopeOptions{
								Global: true,
							},
							Source: managerSource,
						},
						commandio.IO{
							Out: &output,
							Err: &warnings,
						},
					)
				} else {
					installed, installErr := manager.Install(
						t.Context(),
						skills.InstallRequest{
							Source: managerSource,
							Directory: filepath.Join(
								safe,
								"skills",
							),
							Lock: &skills.LockFile{
								Path:   unsafeRoot + string(filepath.Separator) + "skills-lock.json",
								Format: skills.ProjectLock,
							},
						},
					)
					err = installErr
					if len(installed) != 0 {
						t.Fatalf(
							"unsafe installation committed: %v",
							installed,
						)
					}
				}
				if err == nil || !strings.Contains(
					err.Error(),
					"symlink",
				) {
					t.Fatalf(
						"unsafe raw lock path was not rejected: %v",
						err,
					)
				}
				data, readErr := os.ReadFile(filename)
				if readErr != nil || string(data) != initial {
					t.Fatalf(
						"foreign lock changed: %s, %v",
						data,
						readErr,
					)
				}
				for _, path := range []string{
					filepath.Join(
						safe,
						"outside",
						"skills",
					),
					filepath.Join(
						foreign,
						"outside",
						"skills",
					),
				} {
					if exists, statErr := afero.Exists(
						afero.NewOsFs(),
						path,
					); statErr != nil || exists {
						t.Fatalf(
							"XDG path was cleaned or followed: %s, %v, %v",
							path,
							exists,
							statErr,
						)
					}
				}
			},
		)
	}
}
