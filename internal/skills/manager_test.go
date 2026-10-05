package skills_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

const (
	managerSource = "https://example.com/skills.git"
	alphaSkill    = "---\nname: alpha\ndescription: Alpha skill\n---\n# Alpha\n"
	betaSkill     = "---\nname: beta\ndescription: Beta skill\n---\n# Beta\n"
	alphaUpdated  = "---\nname: alpha\ndescription: Updated alpha\n---\n# Updated\n"
)

func TestManagerListSource(t *testing.T) {
	t.Parallel()
	fetchErr := errors.New("source unavailable")
	cleanupErr := errors.New("cleanup failed")
	for _, test := range []struct {
		name                     string
		files                    fstest.MapFS
		options                  skills.DiscoveryOptions
		fetchErr, cleanupErr     error
		cancel, nilClient, nilFS bool
		want                     []skills.Skill
		wantErr                  error
		errText                  string
	}{
		{
			name:  "empty source",
			files: fstest.MapFS{},
		},
		{
			name: "discovers definitions not supporting files",
			files: fstest.MapFS{
				"README.md": {
					Data: []byte("Overview"),
				},
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/scripts/run.sh": {
					Data: []byte("echo alpha\n"),
				},
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "alpha/SKILL.md",
				},
			},
		},
		{
			name: "nested collection",
			files: fstest.MapFS{
				"collections/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "collections/alpha/SKILL.md",
				},
			},
		},
		{
			name: "source root bundle",
			files: fstest.MapFS{
				"SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "SKILL.md",
				},
			},
		},
		{
			name: "deterministic order",
			files: fstest.MapFS{
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "alpha/SKILL.md",
				},
				{
					Name:        "beta",
					Description: "Beta skill",
					Path:        "beta/SKILL.md",
				},
			},
		},
		{
			name: "depth boundary included",
			files: fstest.MapFS{
				"a/b/c/d/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "a/b/c/d/alpha/SKILL.md",
				},
			},
		},
		{
			name: "depth limit",
			files: fstest.MapFS{
				"a/b/c/d/e/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
		},
		{
			name: "full depth",
			files: fstest.MapFS{
				"a/b/c/d/e/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			options: skills.DiscoveryOptions{
				FullDepth: true,
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "a/b/c/d/e/alpha/SKILL.md",
				},
			},
		},
		{
			name: "ignores git metadata",
			files: fstest.MapFS{
				".git/alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
		},
		{
			name: "invalid definition",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			errText: "alpha/SKILL.md",
		},
		{
			name: "mismatched name loads with a violation",
			files: fstest.MapFS{
				"other/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []skills.Skill{
				{
					Name:        "alpha",
					Description: "Alpha skill",
					Path:        "other/SKILL.md",
					Violations: []string{
						`name "alpha" does not match directory "other"`,
					},
				},
			},
		},
		{
			name: "definition is a symlink",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Mode: fs.ModeSymlink,
					Data: []byte("../outside"),
				},
			},
			errText: "regular",
		},
		{
			name:     "fetch failure",
			fetchErr: fetchErr,
			wantErr:  fetchErr,
		},
		{
			name:       "cleanup failure",
			files:      fstest.MapFS{},
			cleanupErr: cleanupErr,
			wantErr:    cleanupErr,
		},
		{
			name:    "cancelled",
			cancel:  true,
			wantErr: context.Canceled,
		},
		{
			name:      "missing client",
			nilClient: true,
			errText:   "source client",
		},
		{
			name:    "missing source filesystem",
			nilFS:   true,
			errText: "source filesystem",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				cleaned := false
				var client skills.SourceClient = managerSourceClient{
					files:   test.files,
					err:     test.fetchErr,
					cleanup: func() error { cleaned = true; return test.cleanupErr },
				}
				if test.nilClient {
					client = nil
				} else if test.nilFS {
					client = managerSourceClient{}
				}
				got, err := skills.NewManager(
					client,
					afero.NewMemMapFs(),
				).ListSource(
					ctx,
					managerSource,
					test.options,
				)
				if test.wantErr != nil {
					if !errors.Is(
						err,
						test.wantErr,
					) {
						t.Fatalf(
							"error = %v, want cause %v",
							err,
							test.wantErr,
						)
					}
				} else if test.errText != "" {
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
				} else if err != nil {
					t.Fatal(err)
				}
				if err != nil {
					if len(got) != 0 {
						t.Fatalf(
							"partial results: %#v",
							got,
						)
					}
				} else if diff := cmp.Diff(
					test.want,
					got,
					cmpopts.EquateEmpty(),
				); diff != "" {
					t.Fatalf(
						"source skills (-want +got):\n%s",
						diff,
					)
				}
				if !test.cancel && !test.nilClient && !test.nilFS && test.fetchErr == nil && !cleaned {
					t.Error("acquired source was not released")
				}
			},
		)
	}
}

func TestManagerInstall(t *testing.T) {
	t.Parallel()
	fetchErr := errors.New("source unavailable")
	for _, test := range []struct {
		name                            string
		files, existing                 fstest.MapFS
		names                           []string
		sourcePath                      *string
		override, committed, recovery   bool
		fault                           managerFSFault
		links                           []string
		fetchErr, cleanupErr            error
		cancel, readOnly, missingRoot   bool
		want                            []string
		violations, installedViolations map[string][]string
		wantErr                         error
		errText                         string
	}{
		{
			name: "selected bundle includes supporting files",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/scripts/run.sh": {
					Data: []byte("echo alpha\n"),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "overlong names remain selectable and persist their violations",
			files: fstest.MapFS{
				strings.Repeat(
					"é",
					65,
				) + "/SKILL.md": {
					Data: []byte("---\nname: " + strings.Repeat(
						"é",
						65,
					) + "\ndescription: valid\n---\n"),
				},
			},
			names: []string{
				strings.Repeat(
					"é",
					65,
				),
			},
			want: []string{
				strings.Repeat(
					"é",
					65,
				),
			},
			violations: map[string][]string{
				strings.Repeat(
					"é",
					65,
				): {
					"name exceeds 64 characters (found 65)",
				},
			},
		},
		{
			name: "declared name selects a differently named source directory",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			names: []string{
				"alpha",
			},
			want: []string{
				"alpha",
			},
			violations: map[string][]string{
				"alpha": {
					`name "alpha" does not match directory "template"`,
				},
			},
			installedViolations: map[string][]string{},
		},
		{
			name: "install all accepts a mismatched source directory",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			want: []string{
				"alpha",
				"beta",
			},
			violations: map[string][]string{
				"alpha": {
					`name "alpha" does not match directory "template"`,
				},
			},
			installedViolations: map[string][]string{},
		},
		{
			name: "exact path accepts a mismatched source directory",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			sourcePath: new("template"),
			want: []string{
				"alpha",
			},
			violations: map[string][]string{
				"alpha": {
					`name "alpha" does not match directory "template"`,
				},
			},
			installedViolations: map[string][]string{},
		},
		{
			name: "directory names do not act as declared-name aliases",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			wantErr: fs.ErrNotExist,
		},
		{
			name: "known unrelated names skip hard validation even in a matching directory",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("---\nname: beta\n---\n"),
				},
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			want: []string{
				"alpha",
			},
			violations: map[string][]string{
				"alpha": {
					`name "alpha" does not match directory "template"`,
				},
			},
			installedViolations: map[string][]string{},
		},
		{
			name: "mismatched directories cannot hide duplicate declared names",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			errText: `ambiguous skill name "alpha" at source directories ["alpha" "template"]; use --path to select one`,
		},
		{
			name: "selected declared name retains hard frontmatter failures",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte("---\nname: alpha\n---\n"),
				},
			},
			names: []string{
				"alpha",
			},
			errText: `missing required field "description"`,
		},
		{
			name: "selected declared name retains hard encoding failures",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill + "\xff"),
				},
			},
			names: []string{
				"alpha",
			},
			errText: "must contain UTF-8",
		},
		{
			name: "selected skill ignores invalid unrelated definitions",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"broken/SKILL.md": {
					Data: []byte("invalid"),
				},
				"template/SKILL.md": {
					Data: []byte("---\nname: template-skill\ndescription: Template\n---\n"),
				},
			},
			names: []string{
				"alpha",
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "all skills",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name: "nested source bundle",
			files: fstest.MapFS{
				"collection/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "root source bundle",
			files: fstest.MapFS{
				"SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "unknown frontmatter fields are preserved and load",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("---\nname: alpha\ndescription: Alpha skill\nhomepage: https://browser-use.com\nmetadata:\n  author: someone\n  openclaw:\n    requires:\n      bins: [browser-use]\n---\n# Alpha\n"),
				},
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "BOM-prefixed installed skill loads",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: append(
						[]byte{
							0xef,
							0xbb,
							0xbf,
						},
						[]byte(alphaSkill)...,
					),
				},
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "duplicate selectors are deduplicated",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
				"alpha",
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "creates destination root",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			missingRoot: true,
			want: []string{
				"alpha",
			},
		},
		{
			name: "preserves unselected existing skill",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "missing selection",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"missing",
			},
			errText: "missing",
		},
		{
			name: "ambiguous selected name reports paths and disambiguation flag",
			files: fstest.MapFS{
				"a/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"b/alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			names: []string{
				"alpha",
			},
			errText: `ambiguous skill name "alpha" at source directories ["a/alpha" "b/alpha"]; use --path to select one`,
		},
		{
			name: "exact source path selects one duplicate with distinct content",
			files: fstest.MapFS{
				"a/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"b/alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			sourcePath: new("b/alpha"),
			want: []string{
				"alpha",
			},
		},
		{
			name: "source path and skill names are mutually exclusive",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			sourcePath: new("alpha"),
			errText:    "--path and --skill are mutually exclusive",
		},
		{
			name:       "explicitly empty source path fails before acquisition",
			sourcePath: new(""),
			fetchErr:   fetchErr,
			errText:    "invalid source directory",
		},
		{
			name: "exact path bypasses discovery depth and ignores unrelated invalid bundles",
			files: fstest.MapFS{
				"broken/SKILL.md": {
					Data: []byte("invalid"),
				},
				"a/b/c/d/e/f/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			sourcePath: new("a/b/c/d/e/f/alpha"),
			want: []string{
				"alpha",
			},
		},
		{
			name: "source root can be selected explicitly",
			files: fstest.MapFS{
				"SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			sourcePath: new("."),
			want: []string{
				"alpha",
			},
		},
		{
			name: "missing exact source path",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			sourcePath: new("missing"),
			wantErr:    fs.ErrNotExist,
		},
		{
			name: "path must identify a skill rather than a collection",
			files: fstest.MapFS{
				"collection/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			sourcePath: new("collection"),
			errText:    "collection/SKILL.md must be a regular definition file",
		},
		{
			name: "path cannot point at the definition file",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			sourcePath: new("alpha/SKILL.md"),
			errText:    "not a directory",
		},
		{
			name: "exact path rejects invalid selected definitions",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			sourcePath: new("alpha"),
			errText:    "parse alpha/SKILL.md",
		},
		{
			name:       "source traversal fails before acquisition",
			sourcePath: new("alpha/../beta"),
			fetchErr:   fetchErr,
			errText:    "invalid source directory",
		},
		{
			name:       "absolute source path fails before acquisition",
			sourcePath: new("/alpha"),
			fetchErr:   fetchErr,
			errText:    "invalid source directory",
		},
		{
			name:       "backslash source path fails before acquisition",
			sourcePath: new(`collection\alpha`),
			fetchErr:   fetchErr,
			errText:    "invalid source directory",
		},
		{
			name: "exact source path rejects symlinked directory components",
			files: fstest.MapFS{
				"link": {
					Mode: fs.ModeSymlink,
					Data: []byte("outside"),
				},
			},
			sourcePath: new("link/alpha"),
			errText:    "symlink",
		},
		{
			name: "exact source path rejects linked definitions",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Mode: fs.ModeSymlink,
					Data: []byte("../outside"),
				},
			},
			sourcePath: new("alpha"),
			errText:    "must be a regular definition file",
		},
		{
			name: "exact path retains payload safety",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/escape": {
					Mode: fs.ModeSymlink,
					Data: []byte("../../outside"),
				},
			},
			sourcePath: new("alpha"),
			errText:    "symlink",
		},
		{
			name: "exact path cleanup failure leaves destination unchanged",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			sourcePath: new("alpha"),
			cleanupErr: fetchErr,
			wantErr:    fetchErr,
		},
		{
			name: "ambiguous names",
			files: fstest.MapFS{
				"a/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"b/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			errText: "ambiguous",
		},
		{
			name: "override also installs absent skills",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			override: true,
			want: []string{
				"alpha",
			},
		},
		{
			name: "override repairs damaged frontmatter",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("broken"),
				},
			},
			override: true,
			want: []string{
				"alpha",
			},
		},
		{
			name: "override replaces the complete selected bundle",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
				"alpha/new.txt": {
					Data: []byte("new"),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/obsolete.txt": {
					Data: []byte("old"),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			override: true,
			want: []string{
				"alpha",
			},
		},
		{
			name: "override rejects a non-skill directory",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/unrelated.txt": {
					Data: []byte("keep"),
				},
			},
			override: true,
			wantErr:  fs.ErrNotExist,
		},
		{
			name: "override rejects an existing file",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha": {
					Data: []byte("keep"),
				},
			},
			override: true,
			errText:  "regular skill directory",
		},
		{
			name: "override rejects a linked directory",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			links: []string{
				"alpha",
			},
			override: true,
			errText:  "regular skill directory",
		},
		{
			name: "override rejects a linked definition",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			links: []string{
				"alpha/SKILL.md",
			},
			override: true,
			errText:  "regular SKILL.md",
		},
		{
			name: "failed replacement restores its original bundle",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/obsolete.txt": {
					Data: []byte("keep"),
				},
			},
			override: true,
			fault:    managerPublishFailure,
			wantErr:  fs.ErrPermission,
		},
		{
			name: "later replacement failure rolls back every bundle",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/obsolete.txt": {
					Data: []byte("keep"),
				},
				"beta/SKILL.md": {
					Data: []byte("broken"),
				},
			},
			override: true,
			fault:    managerLatePublishFailure,
			wantErr:  fs.ErrPermission,
		},
		{
			name: "failed restoration retains recovery data",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			override: true,
			fault:    managerRestoreFailure,
			recovery: true,
			wantErr:  fs.ErrPermission,
		},
		{
			name: "cleanup failure reports committed replacement",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			override:  true,
			fault:     managerStageCleanupFailure,
			committed: true,
			wantErr:   fs.ErrPermission,
			want: []string{
				"alpha",
			},
		},
		{
			name: "rejects existing directory",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			wantErr: fs.ErrExist,
		},
		{
			name: "rejects existing file",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha": {
					Data: []byte("keep"),
				},
			},
			wantErr: fs.ErrExist,
		},
		{
			name: "invalid definition",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			errText: "SKILL.md",
		},
		{
			name: "rejects payload symlink",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"alpha/escape": {
					Mode: fs.ModeSymlink,
					Data: []byte("../../outside"),
				},
			},
			errText: "symlink",
		},
		{
			name:     "source failure",
			fetchErr: fetchErr,
			wantErr:  fetchErr,
		},
		{
			name: "cleanup failure leaves destination unchanged",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			existing: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			override:   true,
			cleanupErr: fetchErr,
			wantErr:    fetchErr,
		},
		{
			name: "read only destination",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			readOnly: true,
			wantErr:  fs.ErrPermission,
		},
		{
			name:    "cancelled",
			cancel:  true,
			wantErr: context.Canceled,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				directory := filepath.FromSlash("/skills")
				base := newManagerFilesystem(
					t,
					directory,
					test.existing,
				)
				if test.missingRoot {
					if err := base.RemoveAll(directory); err != nil {
						t.Fatal(err)
					}
				}
				before := snapshotManagerFS(
					t,
					base,
				)
				destination := base
				if test.fault != managerNoFault {
					destination = managerFaultFS{
						Fs:    destination,
						fault: test.fault,
					}
				}
				if len(test.links) > 0 {
					links := map[string]bool{}
					for _, name := range test.links {
						links[filepath.Join(
							directory,
							filepath.FromSlash(name),
						)] = true
					}
					destination = managerLinkFS{
						Fs:    destination,
						links: links,
					}
				}
				if test.readOnly {
					destination = afero.NewReadOnlyFs(base)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				manager := skills.NewManager(
					managerSourceClient{
						files:   test.files,
						err:     test.fetchErr,
						cleanup: func() error { return test.cleanupErr },
					},
					destination,
				)
				got, err := manager.Install(
					ctx,
					skills.InstallRequest{
						Source:    managerSource,
						Directory: directory,
						Skills:    test.names,
						Path:      test.sourcePath,
						Override:  test.override,
					},
				)
				if test.wantErr != nil {
					if !errors.Is(
						err,
						test.wantErr,
					) {
						t.Fatalf(
							"error = %v, want cause %v",
							err,
							test.wantErr,
						)
					}
				} else if test.errText != "" {
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
				} else if err != nil {
					t.Fatal(err)
				}
				if test.recovery {
					if err == nil || !strings.Contains(
						err.Error(),
						"recovery files retained at",
					) {
						t.Fatalf(
							"missing recovery diagnostic: %v",
							err,
						)
					}
					for name, data := range snapshotManagerFS(
						t,
						base,
					) {
						if strings.HasSuffix(
							filepath.ToSlash(name),
							"/old/alpha/SKILL.md",
						) && data == alphaSkill {
							return
						}
					}
					t.Fatal("original skill data was lost")
				}
				if test.committed {
					if err == nil || !strings.Contains(
						err.Error(),
						"skills changes committed; cleanup failed for ",
					) {
						t.Fatalf(
							"error = %v, want committed cleanup failure",
							err,
						)
					}
				} else if err != nil {
					if diff := cmp.Diff(
						before,
						snapshotManagerFS(
							t,
							base,
						),
					); diff != "" {
						t.Fatalf(
							"failed install changed destination:\n%s",
							diff,
						)
					}
					return
				}
				var names []string
				for _, entry := range got {
					names = append(
						names,
						entry.Skill.Name,
					)
					if entry.Directory != filepath.Join(
						directory,
						entry.Skill.Name,
					) {
						t.Fatalf(
							"bad installation result: %#v",
							entry,
						)
					}
					if diff := cmp.Diff(
						test.violations[entry.Skill.Name],
						entry.Skill.Violations,
						cmpopts.EquateEmpty(),
					); diff != "" {
						t.Fatalf(
							"installation violations (-want +got):\n%s",
							diff,
						)
					}
					sourceFile := ""
					if test.sourcePath != nil {
						sourceFile = filepath.ToSlash(filepath.Join(
							*test.sourcePath,
							"SKILL.md",
						))
					} else {
						for name := range test.files {
							if filepath.Base(name) == "SKILL.md" && strings.Contains(
								string(test.files[name].Data),
								"name: "+entry.Skill.Name+"\n",
							) {
								sourceFile = name
								break
							}
						}
					}
					data, readErr := afero.ReadFile(
						base,
						filepath.Join(
							entry.Directory,
							"SKILL.md",
						),
					)
					if readErr != nil || string(data) != string(test.files[sourceFile].Data) {
						t.Errorf(
							"installed definition = %q, %v",
							data,
							readErr,
						)
					}
				}
				if diff := cmp.Diff(
					test.want,
					names,
					cmpopts.EquateEmpty(),
				); diff != "" {
					t.Fatalf(
						"installed names (-want +got):\n%s",
						diff,
					)
				}
				if len(test.names) > 0 {
					for _, name := range []string{
						"alpha",
						"beta",
					} {
						selected := false
						for _, want := range test.want {
							if want == name {
								selected = true
							}
						}
						if !selected {
							if _, statErr := base.Stat(filepath.Join(
								directory,
								name,
							)); !errors.Is(
								statErr,
								fs.ErrNotExist,
							) {
								t.Errorf(
									"unselected %s directory exists: %v",
									name,
									statErr,
								)
							}
						}
					}
				}
				if len(test.want) > 0 {
					installedViolations := test.violations
					if test.installedViolations != nil {
						installedViolations = test.installedViolations
					}
					if !test.committed {
						loaded, loadErr := skills.Load(afero.NewIOFS(afero.NewBasePathFs(
							base,
							directory,
						)))
						if loadErr != nil {
							t.Fatalf(
								"installed skills fail existing loader: %v",
								loadErr,
							)
						}
						for _, name := range test.want {
							found := false
							for _, skill := range loaded {
								if skill.Name == name {
									found = true
									if diff := cmp.Diff(
										installedViolations[name],
										skill.Violations,
										cmpopts.EquateEmpty(),
									); diff != "" {
										t.Fatalf(
											"loaded violations (-want +got):\n%s",
											diff,
										)
									}
								}
							}
							if !found {
								t.Errorf(
									"installed %s is not loadable",
									name,
								)
							}
						}
					}
					listed, listErr := manager.ListInstalled(
						t.Context(),
						directory,
					)
					if listErr != nil {
						t.Fatal(listErr)
					}
					for _, name := range test.want {
						found := false
						for _, entry := range listed {
							if entry.Skill.Name == name {
								found = true
								if diff := cmp.Diff(
									installedViolations[name],
									entry.Skill.Violations,
									cmpopts.EquateEmpty(),
								); diff != "" {
									t.Fatalf(
										"listed violations (-want +got):\n%s",
										diff,
									)
								}
							}
						}
						if !found {
							t.Errorf(
								"%s installation is not listed",
								name,
							)
						}
					}
				}
				for name, file := range test.files {
					if strings.HasPrefix(
						name,
						"alpha/scripts/",
					) {
						data, readErr := afero.ReadFile(
							base,
							filepath.Join(
								directory,
								filepath.FromSlash(name),
							),
						)
						if readErr != nil || string(data) != string(file.Data) {
							t.Errorf(
								"supporting file %s = %q, %v",
								name,
								data,
								readErr,
							)
						}
					}
				}
				if test.override {
					if _, err := base.Stat(filepath.Join(
						directory,
						"alpha",
						"obsolete.txt",
					)); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Errorf("obsolete payload survived replacement")
					}
				}
				for name, file := range test.existing {
					if test.override && strings.HasPrefix(
						name,
						"alpha/",
					) {
						continue
					}
					data, readErr := afero.ReadFile(
						base,
						filepath.Join(
							directory,
							filepath.FromSlash(name),
						),
					)
					if readErr != nil || string(data) != string(file.Data) {
						t.Errorf(
							"existing file %s changed",
							name,
						)
					}
				}
			},
		)
	}
}

func TestManagerListInstalled(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		files           fstest.MapFS
		missing, cancel bool
		link            string
		want            []string
		errText         string
		wantErr         error
	}{
		{
			name: "empty root",
		},
		{
			name:    "missing root",
			missing: true,
		},
		{
			name: "installed skill",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "ignores files and unrelated directories",
			files: fstest.MapFS{
				"README.md": {
					Data: []byte("hi"),
				},
				"notes/file.txt": {
					Data: []byte("hi"),
				},
			},
		},
		{
			name: "invalid installed definition",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			errText: "SKILL.md",
		},
		{
			name: "symlinked installed directory",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			link: "/skills/alpha",
			want: []string{
				"alpha",
			},
		},
		{
			name: "destination root symlink",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			link:    "/skills",
			errText: "symlink",
		},
		{
			name:    "cancelled",
			cancel:  true,
			wantErr: context.Canceled,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				directory := filepath.FromSlash("/skills")
				destinations := newManagerFilesystem(
					t,
					directory,
					test.files,
				)
				if test.missing {
					if err := destinations.RemoveAll(directory); err != nil {
						t.Fatal(err)
					}
				}
				before := snapshotManagerFS(
					t,
					destinations,
				)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				filesystem := destinations
				if test.link != "" {
					filesystem = managerLinkFS{
						Fs: destinations,
						links: map[string]bool{
							test.link: true,
						},
					}
				}
				got, err := skills.NewManager(
					nil,
					filesystem,
				).ListInstalled(
					ctx,
					directory,
				)
				if test.wantErr != nil {
					if !errors.Is(
						err,
						test.wantErr,
					) {
						t.Fatalf(
							"error = %v, want cause %v",
							err,
							test.wantErr,
						)
					}
				} else if test.errText != "" {
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
				} else if err != nil {
					t.Fatal(err)
				}
				if err == nil {
					var names []string
					for _, entry := range got {
						names = append(
							names,
							entry.Skill.Name,
						)
						if entry.Directory != filepath.Join(
							directory,
							entry.Skill.Name,
						) {
							t.Errorf(
								"directory: %q",
								entry.Directory,
							)
						}
					}
					if diff := cmp.Diff(
						test.want,
						names,
						cmpopts.EquateEmpty(),
					); diff != "" {
						t.Error(diff)
					}
				}
				if diff := cmp.Diff(
					before,
					snapshotManagerFS(
						t,
						destinations,
					),
				); diff != "" {
					t.Fatalf(
						"listing mutated files:\n%s",
						diff,
					)
				}
			},
		)
	}
}

func TestManagerRemove(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		files            fstest.MapFS
		names, want      []string
		wantName         string
		wantDescription  *string
		cancel, readOnly bool
		errText          string
		wantErr          error
	}{
		{
			name: "selected installed skill",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "duplicate selectors",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
				"alpha",
			},
			want: []string{
				"alpha",
			},
		},
		{
			name: "mismatched removal returns the declared name and actual path",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"template",
			},
			want: []string{
				"template",
			},
			wantName: "alpha",
		},
		{
			name:            "damaged installed bundle",
			wantDescription: new(""),
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			names: []string{
				"alpha",
			},
			want: []string{
				"alpha",
			},
		},
		{
			name:    "no names",
			errText: "select",
		},
		{
			name: "missing name preflight preserves other selections",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
				"missing",
			},
			wantErr: fs.ErrNotExist,
		},
		{
			name: "path traversal",
			names: []string{
				"../outside",
			},
			errText: "name",
		},
		{
			name: "absolute selector",
			names: []string{
				"/outside",
			},
			errText: "name",
		},
		{
			name: "unrelated directory is preserved",
			files: fstest.MapFS{
				"notes/file.txt": {
					Data: []byte("keep"),
				},
			},
			names: []string{
				"notes",
			},
			errText: "skill directory",
		},
		{
			name: "file is not a skill directory",
			files: fstest.MapFS{
				"alpha": {
					Data: []byte("keep"),
				},
			},
			names: []string{
				"alpha",
			},
			errText: "directory",
		},
		{
			name: "read only",
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			names: []string{
				"alpha",
			},
			readOnly: true,
			wantErr:  fs.ErrPermission,
		},
		{
			name:   "cancelled",
			cancel: true,
			names: []string{
				"alpha",
			},
			wantErr: context.Canceled,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				directory := filepath.FromSlash("/skills")
				base := newManagerFilesystem(
					t,
					directory,
					test.files,
				)
				before := snapshotManagerFS(
					t,
					base,
				)
				destinations := base
				if test.readOnly {
					destinations = afero.NewReadOnlyFs(base)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if test.cancel {
					cancel()
				}
				got, err := skills.NewManager(
					nil,
					destinations,
				).Remove(
					ctx,
					skills.RemoveRequest{
						Directory: directory,
						Skills:    test.names,
					},
				)
				if test.wantErr != nil {
					if !errors.Is(
						err,
						test.wantErr,
					) {
						t.Fatalf(
							"error = %v, want cause %v",
							err,
							test.wantErr,
						)
					}
				} else if test.errText != "" {
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
				} else if err != nil {
					t.Fatal(err)
				}
				if err != nil {
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
				var removed []skills.RemovedSkill
				for _, name := range test.want {
					description := "Alpha skill"
					if test.wantDescription != nil {
						description = *test.wantDescription
					}
					declaredName := name
					if test.wantName != "" {
						declaredName = test.wantName
					}
					removed = append(
						removed,
						skills.RemovedSkill{
							Name:        declaredName,
							Description: description,
							Path: filepath.Join(
								directory,
								name,
								"SKILL.md",
							),
						},
					)
					if _, statErr := base.Stat(filepath.Join(
						directory,
						name,
					)); !errors.Is(
						statErr,
						fs.ErrNotExist,
					) {
						t.Errorf(
							"removed bundle %s still exists: %v",
							name,
							statErr,
						)
					}
				}
				if diff := cmp.Diff(
					removed,
					got,
				); diff != "" {
					t.Error(diff)
				}
				if file, ok := test.files["beta/SKILL.md"]; ok {
					data, readErr := afero.ReadFile(
						base,
						filepath.Join(
							directory,
							"beta",
							"SKILL.md",
						),
					)
					if readErr != nil || string(data) != string(file.Data) {
						t.Errorf("unselected bundle changed")
					}
				}
			},
		)
	}
}

// Source acquisition is the only mocked Manager collaborator.
type managerSourceClient struct {
	files   fs.FS
	err     error
	cleanup func() error
}

func (client managerSourceClient) Fetch(_ context.Context, source string) (skills.Checkout, error) {
	if source != managerSource {
		return skills.Checkout{}, errors.New("unexpected source locator: " + source)
	}
	if client.err != nil {
		return skills.Checkout{}, client.err
	}
	return skills.Checkout{
		FS:      client.files,
		Cleanup: client.cleanup,
	}, nil
}
func newManagerFilesystem(t *testing.T, root string, files fstest.MapFS) afero.Fs {
	t.Helper()
	filesystem := afero.NewMemMapFs()
	if err := filesystem.MkdirAll(
		root,
		0o700,
	); err != nil {
		t.Fatal(err)
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
	return filesystem
}
func snapshotManagerFS(t *testing.T, filesystem afero.Fs) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := afero.Walk(
		filesystem,
		string(filepath.Separator),
		func(name string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				result[name] = "<directory>"
				return nil
			}
			data, err := afero.ReadFile(
				filesystem,
				name,
			)
			if err != nil {
				return err
			}
			result[name] = string(data)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
