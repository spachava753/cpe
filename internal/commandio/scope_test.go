package commandio_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/commandio"
)

func TestScopeDirectories(t *testing.T) {
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
	empty, relative := "", "custom"
	raw := project + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "custom"
	projectRoot := filepath.Join(
		project,
		".agents",
		"widgets",
	)
	globalRoot := filepath.Join(
		home,
		".agents",
		"widgets",
	)
	type scopeCase struct {
		name         string
		scope        commandio.ScopeOptions
		requireScope bool
		noHome       bool
		want         []string
		errText      string
	}
	cases := []scopeCase{
		{
			name: "both default roots",
			want: []string{
				projectRoot,
				globalRoot,
			},
		},
		{
			name: "project root",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			want: []string{
				projectRoot,
			},
		},
		{
			name: "global root",
			scope: commandio.ScopeOptions{
				Global: true,
			},
			want: []string{
				globalRoot,
			},
		},
		{
			name: "conflicting roots",
			scope: commandio.ScopeOptions{
				Project: true,
				Global:  true,
			},
			errText: "conflicting",
		},
		{
			name:         "installation requires scope",
			requireScope: true,
			errText:      "scope is required",
		},
		{
			name: "custom install requires project scope",
			scope: commandio.ScopeOptions{
				Directory: &relative,
			},
			requireScope: true,
			errText:      "scope is required",
		},
		{
			name: "relative custom root",
			scope: commandio.ScopeOptions{
				Directory: &relative,
			},
			want: []string{
				filepath.Join(
					project,
					"custom",
				),
			},
		},
		{
			name: "raw absolute custom root",
			scope: commandio.ScopeOptions{
				Directory: &raw,
			},
			want: []string{
				raw,
			},
		},
		{
			name: "custom project install",
			scope: commandio.ScopeOptions{
				Directory: &raw,
				Project:   true,
			},
			requireScope: true,
			want: []string{
				raw,
			},
		},
		{
			name: "custom global root is invalid",
			scope: commandio.ScopeOptions{
				Directory: &raw,
				Global:    true,
			},
			errText: "cannot be combined",
		},
		{
			name: "empty custom root",
			scope: commandio.ScopeOptions{
				Directory: &empty,
			},
			errText: "must not be empty",
		},
		{
			name: "missing home does not block custom root",
			scope: commandio.ScopeOptions{
				Directory: &raw,
			},
			noHome: true,
			want: []string{
				raw,
			},
		},
		{
			name: "missing home does not block project root",
			scope: commandio.ScopeOptions{
				Project: true,
			},
			noHome: true,
			want: []string{
				projectRoot,
			},
		},
		{
			name: "missing home rejects global root",
			scope: commandio.ScopeOptions{
				Global: true,
			},
			noHome:  true,
			errText: "is not defined",
		},
	}
	if runtime.GOOS == "windows" {
		driveRelative, rootRelative, forwardRoot := "C:custom", "\\custom", "/custom"
		cases = append(
			cases,
			scopeCase{
				name: "drive-relative custom root",
				scope: commandio.ScopeOptions{
					Directory: &driveRelative,
				},
				errText: "drive-relative",
			},
			scopeCase{
				name: "native rooted custom path stays rooted",
				scope: commandio.ScopeOptions{
					Directory: &rootRelative,
				},
				want: []string{
					rootRelative,
				},
			},
			scopeCase{
				name: "forward rooted custom path stays rooted",
				scope: commandio.ScopeOptions{
					Directory: &forwardRoot,
				},
				want: []string{
					forwardRoot,
				},
			},
		)
	}
	for _, test := range cases {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.noHome {
					t.Setenv(
						"HOME",
						"",
					)
					t.Setenv(
						"USERPROFILE",
						"",
					)
				}
				actual, err := test.scope.Directories(
					"widgets",
					test.requireScope,
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
					if actual != nil {
						t.Fatalf(
							"invalid scope returned partial roots: %v",
							actual,
						)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(
					actual,
					test.want,
				) {
					t.Fatalf(
						"roots = %v, want %v",
						actual,
						test.want,
					)
				}
			},
		)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != project {
		t.Fatalf(
			"scope resolution changed cwd: %q, %v",
			cwd,
			err,
		)
	}
}
