package skills

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/spachava753/cpe/internal/commandio"
)

func TestResolveScopes(t *testing.T) {
	project, home, state := t.TempDir(), t.TempDir(), t.TempDir()
	t.Chdir(project)
	t.Setenv(
		"HOME",
		home,
	)
	t.Setenv(
		"USERPROFILE",
		home,
	)
	for _, test := range []struct {
		name      string
		options   commandio.ScopeOptions
		stateHome string
		want      []skillScope
	}{
		{
			name: "default inventories use separate upstream lock formats",
			want: []skillScope{
				{
					directory: filepath.Join(
						project,
						".agents",
						"skills",
					),
					lock: LockFile{
						Path: filepath.Join(
							project,
							"skills-lock.json",
						),
						Format: ProjectLock,
					},
				},
				{
					directory: filepath.Join(
						home,
						".agents",
						"skills",
					),
					lock: LockFile{
						Path: filepath.Join(
							home,
							".agents",
							".skill-lock.json",
						),
						Format: GlobalLock,
					},
				},
			},
		},
		{
			name: "XDG state moves global lock but not skill installation",
			options: commandio.ScopeOptions{
				Global: true,
			},
			stateHome: state,
			want: []skillScope{
				{
					directory: filepath.Join(
						home,
						".agents",
						"skills",
					),
					lock: LockFile{
						Path: filepath.Join(
							state,
							"skills",
							".skill-lock.json",
						),
						Format: GlobalLock,
					},
				},
			},
		},
		{
			name: "project lock ignores XDG state",
			options: commandio.ScopeOptions{
				Project: true,
			},
			stateHome: state,
			want: []skillScope{
				{
					directory: filepath.Join(
						project,
						".agents",
						"skills",
					),
					lock: LockFile{
						Path: filepath.Join(
							project,
							"skills-lock.json",
						),
						Format: ProjectLock,
					},
				},
			},
		},
		{
			name: "custom directory has isolated lock",
			options: commandio.ScopeOptions{
				Directory: new("custom"),
			},
			want: []skillScope{
				{
					directory: filepath.Join(
						project,
						"custom",
					),
					lock: LockFile{
						Path: filepath.Join(
							project,
							"custom",
							"skills-lock.json",
						),
						Format: ProjectLock,
					},
				},
			},
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Setenv(
					"XDG_STATE_HOME",
					test.stateHome,
				)
				got, err := resolveScopes(
					test.options,
					false,
				)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(
					test.want,
					got,
					cmp.AllowUnexported(skillScope{}),
				); diff != "" {
					t.Fatal(diff)
				}
			},
		)
	}
}
