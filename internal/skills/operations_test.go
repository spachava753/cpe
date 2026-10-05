package skills_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/skills"
)

func TestInstall(t *testing.T) {
	outputErr := errors.New("output unavailable")
	warningErr := errors.New("warnings unavailable")
	for _, test := range []struct {
		name             string
		scope            skills.ScopeOptions
		source           string
		names            []string
		sourcePath       *string
		sourceFiles      fstest.MapFS
		existing         fstest.MapFS
		override         bool
		fault            managerFSFault
		committed        bool
		wantDefinition   string
		deep             bool
		link             bool
		json             bool
		nonTTY, pipe     bool
		empty            bool
		errText          string
		outputErr        error
		forceColor       bool
		description      string
		otherDescription string
		invalidOther     bool
		warningErr       error
		wantWarnings     []string
		wantText         []string
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
			name: "override replaces an existing skill",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
			existing: fstest.MapFS{
				"custom/alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			override: true,
		},
		{
			name: "committed replacement is rendered despite cleanup failure",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
			existing: fstest.MapFS{
				"custom/alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			override:  true,
			fault:     managerStageCleanupFailure,
			committed: true,
		},
		{
			name: "non-terminal install automatically receives a summary array",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			nonTTY: true,
			names: []string{
				"alpha",
			},
		},
		{
			name: "pipe install output remains JSON with forced color",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:     managerSource,
			nonTTY:     true,
			pipe:       true,
			forceColor: true,
		},
		{
			name: "committed cleanup failure still emits an automatic JSON array",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:    managerSource,
			nonTTY:    true,
			fault:     managerStageCleanupFailure,
			committed: true,
			names: []string{
				"alpha",
			},
		},
		{
			name: "automatic JSON excludes non-JSON metadata without losing bundle contents",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			nonTTY: true,
			names: []string{
				"alpha",
			},
			sourceFiles: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("---\nname: alpha\ndescription: Alpha skill\nmetadata:\n  value: .nan\n---\n"),
				},
			},
			wantDefinition: "---\nname: alpha\ndescription: Alpha skill\nmetadata:\n  value: .nan\n---\n",
		},
		{
			name: "automatic JSON preserves description whitespace and terminal controls",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:      managerSource,
			nonTTY:      true,
			description: " \t\"Alpha\" \\\nnext \x1b[31mred\x1b[0m ",
			names: []string{
				"alpha",
			},
		},
		{
			name: "automatic JSON keeps warnings on stderr",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			nonTTY: true,
			description: strings.Repeat(
				"x",
				1025,
			),
			names: []string{
				"alpha",
			},
			wantWarnings: []string{
				"alpha: description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name: "empty source produces an empty JSON array without mutation",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:      managerSource,
			sourceFiles: fstest.MapFS{},
			nonTTY:      true,
			empty:       true,
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
			name: "colored install result",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:     managerSource,
			forceColor: true,
		},
		{
			name: "install keeps readable descriptions and neutralizes terminal commands",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:      managerSource,
			forceColor:  true,
			description: " \n\tAlpha skill says \"quoted\" in C:\\work.\nnext\tline\x1b[2J\x1b[H\a\r\b\x00. \n ",
			wantText: []string{
				"Alpha skill says \"quoted\" in C:\\work.",
				"next    line.",
			},
		},
		{
			name: "exact source path installs the requested duplicate",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:     managerSource,
			sourcePath: new("second/alpha"),
			sourceFiles: fstest.MapFS{
				"first/alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"second/alpha/SKILL.md": {
					Data: []byte(alphaUpdated),
				},
			},
			wantDefinition: alphaUpdated,
			wantText: []string{
				"Updated alpha",
			},
		},
		{
			name: "command rejects both selector kinds",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:     managerSource,
			sourcePath: new("alpha"),
			names: []string{
				"alpha",
			},
			errText: "--path and --skill are mutually exclusive",
		},
		{
			name: "command rejects explicitly empty path",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source:     managerSource,
			sourcePath: new(""),
			errText:    "invalid source directory",
		},
		{
			name: "install all warns for a directory mismatch without blocking",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			sourceFiles: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
			wantWarnings: []string{
				`alpha: name "alpha" does not match directory "template"`,
			},
		},
		{
			name: "name-selected installation warns for its mismatched directory",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
			sourceFiles: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"beta/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
			wantWarnings: []string{
				`alpha: name "alpha" does not match directory "template"`,
			},
		},
		{
			name: "unselected mismatched directories do not produce warnings",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
			sourceFiles: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"template/SKILL.md": {
					Data: []byte(betaSkill),
				},
			},
		},
		{
			name: "selected installation warns only for its skill",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
			description: strings.Repeat(
				"é",
				1025,
			),
			invalidOther: true,
			wantWarnings: []string{
				"alpha: description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name: "unselected soft violations do not produce installation warnings",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			names: []string{
				"alpha",
			},
			otherDescription: strings.Repeat(
				"x",
				2048,
			),
		},
		{
			name: "install all warns for every installed skill",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			description: strings.Repeat(
				"é",
				1025,
			),
			otherDescription: strings.Repeat(
				"x",
				1030,
			),
			wantWarnings: []string{
				"alpha: description exceeds 1024 characters (found 1025)",
				"beta: description exceeds 1024 characters (found 1030)",
			},
		},
		{
			name: "warning output failure preserves its cause and committed result",
			scope: skills.ScopeOptions{
				Project:   true,
				Directory: new("/custom"),
			},
			source: managerSource,
			description: strings.Repeat(
				"é",
				1025,
			),
			warningErr: warningErr,
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
				t.Setenv(
					"CLICOLOR_FORCE",
					"",
				)
				t.Setenv(
					"NO_COLOR",
					"",
				)
				if test.forceColor {
					t.Setenv(
						"CLICOLOR_FORCE",
						"1",
					)
				}
				destinations := newManagerFilesystem(
					t,
					"/",
					test.existing,
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
				definition := alphaSkill
				if test.description != "" {
					definition = strings.ReplaceAll(
						alphaSkill,
						"Alpha skill",
						strconv.Quote(test.description),
					)
				}
				if test.fault != managerNoFault {
					destinations = managerFaultFS{
						Fs:    destinations,
						fault: test.fault,
					}
				}
				otherDefinition := betaSkill
				if test.otherDescription != "" {
					otherDefinition = strings.ReplaceAll(
						betaSkill,
						"Beta skill",
						strconv.Quote(test.otherDescription),
					)
				}
				if test.invalidOther {
					otherDefinition = "invalid"
				}
				files := fstest.MapFS{
					"alpha/SKILL.md": {
						Data: []byte(definition),
					},
					"beta/SKILL.md": {
						Data: []byte(otherDefinition),
					},
				}
				if test.sourceFiles != nil {
					files = test.sourceFiles
				}
				if test.deep {
					files = fstest.MapFS{
						"a/b/c/d/e/alpha/SKILL.md": {
							Data: []byte(definition),
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
				var output, diagnostics bytes.Buffer
				streams := skills.IO{
					Out: &output,
					Err: &diagnostics,
				}
				if test.warningErr != nil {
					streams.Err = commandOutputFailure{
						err: test.warningErr,
					}
				}
				if test.outputErr != nil {
					streams.Out = commandOutputFailure{
						err: test.outputErr,
					}
				}
				if !test.forceColor {
					t.Setenv(
						"NO_COLOR",
						"1",
					)
				}
				if !test.nonTTY && test.errText == "" {
					streams.Out = commandTerminal(
						t,
						streams.Out,
					)
				} else if test.pipe {
					streams.Out = commandPipe(
						t,
						streams.Out,
					)
				}
				asJSON := test.json || test.nonTTY
				err := skills.Install(
					t.Context(),
					manager,
					skills.InstallOptions{
						Scope:     test.scope,
						Source:    test.source,
						Skills:    test.names,
						Path:      test.sourcePath,
						Override:  test.override,
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
				} else if test.warningErr != nil {
					if !errors.Is(
						err,
						test.warningErr,
					) {
						t.Fatalf(
							"error = %v, want original warning writer failure",
							err,
						)
					}
				} else if test.committed {
					var cleanup *skills.CommittedCleanupError
					if !errors.As(
						err,
						&cleanup,
					) || !errors.Is(
						err,
						fs.ErrPermission,
					) {
						t.Fatalf(
							"error = %v, want committed cleanup failure with its cause",
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
				if !test.empty {
					data, readErr := afero.ReadFile(
						destinations,
						filepath.Join(
							root,
							"alpha",
							"SKILL.md",
						),
					)
					wantDefinition := definition
					if test.wantDefinition != "" {
						wantDefinition = test.wantDefinition
					}
					if readErr != nil || string(data) != wantDefinition {
						t.Errorf(
							"installation at selected root = %q, %v",
							data,
							readErr,
						)
					}
				}
				if test.empty {
					if diff := cmp.Diff(
						before,
						snapshotManagerFS(
							t,
							destinations,
						),
					); diff != "" {
						t.Fatalf(
							"empty install changed files:\n%s",
							diff,
						)
					}
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
				if count := strings.Count(
					diagnostics.String(),
					"\n",
				); count != len(test.wantWarnings) {
					t.Errorf(
						"warning count = %d, want %d: %q",
						count,
						len(test.wantWarnings),
						&diagnostics,
					)
				}
				for _, warning := range test.wantWarnings {
					if !strings.Contains(
						diagnostics.String(),
						warning,
					) || strings.Contains(
						output.String(),
						"Warning:",
					) {
						t.Errorf(
							"warning %q not confined to stderr: stdout=%q, stderr=%q",
							warning,
							&output,
							&diagnostics,
						)
					}
				}
				if asJSON {
					var entries []map[string]any
					if err := json.Unmarshal(
						output.Bytes(),
						&entries,
					); err != nil {
						t.Fatal(err)
					}
					wantNames := test.names
					if test.empty {
						wantNames = []string{}
						if strings.TrimSpace(output.String()) != "[]" {
							t.Fatalf(
								"empty install output = %q",
								&output,
							)
						}
					}
					if wantNames == nil {
						wantNames = []string{
							"alpha",
							"beta",
						}
					}
					if len(entries) != len(wantNames) {
						t.Fatalf(
							"installed summaries = %#v, want %d skills",
							entries,
							len(wantNames),
						)
					}
					for i, entry := range entries {
						name := wantNames[i]
						wantDescription := "Alpha skill"
						if name == "beta" {
							wantDescription = "Beta skill"
							if test.otherDescription != "" {
								wantDescription = test.otherDescription
							}
						} else if test.description != "" {
							wantDescription = test.description
						}
						if len(entry) != 3 || entry["name"] != name || entry["description"] != wantDescription ||
							entry["path"] != filepath.Join(
								root,
								name,
								"SKILL.md",
							) {
							t.Errorf(
								"installed summary = %#v",
								entry,
							)
						}
					}
				} else {
					for _, text := range []string{
						"Skill",
						"Description",
						"Location",
						"alpha",
						filepath.Join(
							root,
							"alpha",
						),
					} {
						if !strings.Contains(
							output.String(),
							text,
						) {
							t.Errorf(
								"missing installed detail %q: %q",
								text,
								&output,
							)
						}
					}
					if test.description == "" && test.wantDefinition == "" && !strings.Contains(
						output.String(),
						"Alpha skill",
					) {
						t.Errorf(
							"missing installed description: %q",
							&output,
						)
					}
					if strings.Contains(
						output.String(),
						"\x1b[",
					) != test.forceColor {
						t.Errorf(
							"unexpected styling in installation output: %q",
							&output,
						)
					}
					for _, text := range test.wantText {
						if !strings.Contains(
							output.String(),
							text,
						) {
							t.Errorf(
								"missing readable install text %q: %q",
								text,
								&output,
							)
						}
					}
					if strings.ContainsAny(
						output.String(),
						"\a\r\b\x00",
					) || strings.Contains(
						output.String(),
						"\x1b[2J",
					) || strings.Contains(
						output.String(),
						"\x1b[H",
					) {
						t.Errorf(
							"untrusted controls reached install output: %q",
							&output,
						)
					}
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
	warningErr := errors.New("warnings unavailable")
	for _, test := range []struct {
		name              string
		scope             skills.ScopeOptions
		source            *string
		json              bool
		nonTTY, pipe      bool
		empty             bool
		want              []string
		unsafeDescription bool
		directoryMismatch bool
		description       string
		invocation        *bool
		wantText          []string
		wantLineCount     int
		wantWarnings      []string
		warningErr        error
		nestedMetadata    bool
		nonJSONMetadata   bool
		forceColor        bool
		noColor           bool
	}{
		{
			name: "both scopes by default",
			want: []string{
				"alpha",
				"beta",
			},
			wantText: []string{
				"Enabled",
			},
		},
		{
			name: "installed JSON reports disabled model invocation",
			scope: skills.ScopeOptions{
				Project: true,
			},
			json:       true,
			invocation: new(true),
			want: []string{
				"alpha",
			},
			wantText: []string{
				`"disable-model-invocation":true`,
			},
		},
		{
			name: "installed table marks user-only skills as disabled",
			scope: skills.ScopeOptions{
				Project: true,
			},
			invocation: new(true),
			want: []string{
				"alpha",
			},
			wantText: []string{
				"Model invocation",
				"Disabled",
			},
		},
		{
			name:       "source table marks user-only skills as disabled",
			source:     new(managerSource),
			invocation: new(true),
			want: []string{
				"delta",
			},
			wantText: []string{
				"Model invocation",
				"Disabled",
			},
		},
		{
			name:       "source JSON reports disabled model invocation",
			source:     new(managerSource),
			json:       true,
			invocation: new(true),
			want: []string{
				"delta",
			},
		},
		{
			name: "explicit false permits model invocation",
			scope: skills.ScopeOptions{
				Project: true,
			},
			invocation: new(false),
			want: []string{
				"alpha",
			},
			wantText: []string{
				"Enabled",
			},
		},
		{
			name:       "invocation status is per skill in mixed JSON inventory",
			json:       true,
			invocation: new(true),
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name: "empty inventory is an empty JSON array",
			scope: skills.ScopeOptions{
				Project: true,
			},
			json:  true,
			empty: true,
			wantText: []string{
				"[]",
			},
		},
		{
			name:   "non-terminal writer automatically receives JSON",
			nonTTY: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:       "pipe output receives JSON even with forced color",
			nonTTY:     true,
			pipe:       true,
			forceColor: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:   "source non-terminal output automatically receives JSON",
			source: new(managerSource),
			nonTTY: true,
			want: []string{
				"delta",
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
			name: "installed directory mismatch warns without blocking inventory",
			scope: skills.ScopeOptions{
				Project: true,
			},
			directoryMismatch: true,
			want: []string{
				"alpha",
			},
			wantWarnings: []string{
				`alpha: name "alpha" does not match directory "template"`,
			},
		},
		{
			name:              "installed JSON reports directory mismatch warnings on stderr",
			json:              true,
			directoryMismatch: true,
			description:       "Alpha skill",
			want: []string{
				"alpha",
				"beta",
			},
			wantWarnings: []string{
				`alpha: name "alpha" does not match directory "template"`,
			},
		},
		{
			name:              "source directory mismatch warns without blocking inventory",
			source:            new(managerSource),
			directoryMismatch: true,
			want: []string{
				"delta",
			},
			wantWarnings: []string{
				`delta: name "delta" does not match directory "template"`,
			},
		},
		{
			name:              "source JSON reports directory mismatch warnings on stderr",
			source:            new(managerSource),
			json:              true,
			directoryMismatch: true,
			description:       "Delta skill",
			want: []string{
				"delta",
			},
			wantWarnings: []string{
				`delta: name "delta" does not match directory "template"`,
			},
		},
		{
			name: "installed length violations warn on stderr without failing",
			scope: skills.ScopeOptions{
				Project: true,
			},
			description: strings.Repeat(
				"é",
				1025,
			),
			want: []string{
				"alpha",
			},
			wantWarnings: []string{
				"alpha: description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name: "installed warning writer failure preserves cause and listing",
			scope: skills.ScopeOptions{
				Project: true,
			},
			description: strings.Repeat(
				"é",
				1025,
			),
			warningErr: warningErr,
			want: []string{
				"alpha",
			},
		},
		{
			name:   "source warning writer failure preserves cause and listing",
			source: new(managerSource),
			description: strings.Repeat(
				"é",
				1025,
			),
			warningErr: warningErr,
			want: []string{
				"delta",
			},
		},
		{
			name:              "terminal controls removed",
			unsafeDescription: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:          "quotes and inner whitespace remain readable",
			wantLineCount: 5,
			description:   " \n\t Mentions \"red-green-refactor\", or wants\n\tintegration tests in C:\\work. \n ",
			want: []string{
				"alpha",
				"beta",
			},
			wantText: []string{
				"\nalpha  Mentions \"red-green-refactor\", or wants",
				"\n           integration tests in C:\\work.",
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
			name:              "colored inventory removes untrusted controls",
			forceColor:        true,
			unsafeDescription: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:        "terminal instructions and non-layout controls are removed",
			description: " Alpha \x1b[2J\x1b[H\x1b[31mred\x1b[0m \x1b]8;;https://evil.invalid\x1b\\link\x1b]8;;\x1b\\ \x1b]52;c;ZXZpbA==\a safe\a\r\b\x00\v\f\x7f\u0085END \n",
			forceColor:  true,
			want: []string{
				"alpha",
				"beta",
			},
			wantText: []string{
				"Alpha red link  safeEND",
			},
		},
		{
			name:    "NO_COLOR inventory",
			noColor: true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:       "JSON is unstyled even when color is forced",
			json:       true,
			forceColor: true,
			want: []string{
				"alpha",
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
			name:   "source length violations warn without failing",
			source: new(managerSource),
			description: strings.Repeat(
				"é",
				1025,
			),
			want: []string{
				"delta",
			},
			wantWarnings: []string{
				"delta: description exceeds 1024 characters (found 1025)",
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
			name:          "source quotes and inner whitespace remain readable",
			wantLineCount: 4,
			source:        new(managerSource),
			description:   " \n\t Mentions \"red-green-refactor\", or wants\n\tintegration tests in C:\\work. \n ",
			want: []string{
				"delta",
			},
			wantText: []string{
				"\ndelta  Mentions \"red-green-refactor\", or wants",
				"\n           integration tests in C:\\work.",
			},
		},
		{
			name:        "colored source strips terminal instructions",
			source:      new(managerSource),
			description: "Delta\x1b[2J\x1b[H\x1b]52;c;ZXZpbA==\a safe\r\b\x00\u0085END",
			forceColor:  true,
			want: []string{
				"delta",
			},
			wantText: []string{
				"Delta safeEND",
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
			name: "compact JSON inventory keeps warnings on stderr",
			json: true,
			description: strings.Repeat(
				"é",
				1025,
			),
			forceColor: true,
			want: []string{
				"alpha",
				"beta",
			},
			wantWarnings: []string{
				"alpha: description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name:   "compact source JSON keeps warnings on stderr",
			source: new(managerSource),
			json:   true,
			description: strings.Repeat(
				"é",
				1025,
			),
			forceColor: true,
			want: []string{
				"delta",
			},
			wantWarnings: []string{
				"delta: description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name:        "JSON preserves original description content",
			json:        true,
			description: " \tAlpha \"quoted\"\\path\n\tline.\x1b[2J\a\r \n",
			forceColor:  true,
			want: []string{
				"alpha",
				"beta",
			},
		},
		{
			name:        "source JSON preserves original description content",
			source:      new(managerSource),
			json:        true,
			description: " \tDelta \"quoted\"\\path\n\tline.\x1b[2J\a\r \n",
			forceColor:  true,
			want: []string{
				"delta",
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
			name:           "compact JSON excludes nested metadata",
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
			name:            "compact JSON excludes non-JSON metadata",
			nonJSONMetadata: true,
			json:            true,
			want: []string{
				"alpha",
				"beta",
			},
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
				t.Setenv(
					"CLICOLOR_FORCE",
					"",
				)
				t.Setenv(
					"NO_COLOR",
					"",
				)
				if test.forceColor {
					t.Setenv(
						"CLICOLOR_FORCE",
						"1",
					)
				}
				if test.noColor || !test.forceColor {
					t.Setenv(
						"NO_COLOR",
						"1",
					)
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
				if test.description != "" {
					definition = strings.ReplaceAll(
						alphaSkill,
						"Alpha skill",
						strconv.Quote(test.description),
					)
				}
				if test.invocation != nil {
					definition = strings.Replace(
						definition,
						"\n---\n",
						"\ndisable-model-invocation: "+strconv.FormatBool(*test.invocation)+"\n---\n",
						1,
					)
				}
				projectDirectory := "alpha"
				sourceDirectory := "delta"
				if test.directoryMismatch {
					projectDirectory = "template"
					sourceDirectory = "template"
				}
				projectFiles := fstest.MapFS{
					projectDirectory + "/SKILL.md": {
						Data: []byte(definition),
					},
				}
				if test.empty {
					projectFiles = fstest.MapFS{}
				}
				destinations := newManagerFilesystem(
					t,
					skills.ProjectPath(cwd),
					projectFiles,
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
				sourceDefinition := "---\nname: delta\ndescription: \"Delta\\e]52;c;evil\\a\"\n---\n"
				if test.description != "" {
					sourceDefinition = "---\nname: delta\ndescription: " + strconv.Quote(test.description) + "\n---\n"
				}
				if test.invocation != nil {
					sourceDefinition = strings.Replace(
						sourceDefinition,
						"\n---\n",
						"\ndisable-model-invocation: "+strconv.FormatBool(*test.invocation)+"\n---\n",
						1,
					)
				}
				manager := skills.NewManager(
					managerSourceClient{
						files: fstest.MapFS{
							sourceDirectory + "/SKILL.md": {
								Data: []byte(sourceDefinition),
							},
						},
					},
					destinations,
				)
				var output, diagnostics bytes.Buffer
				var resultOutput io.Writer = &output
				if !test.nonTTY {
					resultOutput = commandTerminal(
						t,
						resultOutput,
					)
				} else if test.pipe {
					resultOutput = commandPipe(
						t,
						resultOutput,
					)
				}
				asJSON := test.json || test.nonTTY
				var warningOutput io.Writer = &diagnostics
				if test.warningErr != nil {
					warningOutput = commandOutputFailure{
						err: test.warningErr,
					}
				}
				err = skills.List(
					t.Context(),
					manager,
					skills.ListOptions{
						Scope:  test.scope,
						Source: test.source,
						JSON:   test.json,
					},
					skills.IO{
						Out: resultOutput,
						Err: warningOutput,
					},
				)
				if test.warningErr != nil {
					if !errors.Is(
						err,
						test.warningErr,
					) {
						t.Fatalf(
							"error = %v, want original warning writer failure",
							err,
						)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if count := strings.Count(
					diagnostics.String(),
					"\n",
				); count != len(test.wantWarnings) {
					t.Errorf(
						"warning count = %d, want %d: %q",
						count,
						len(test.wantWarnings),
						&diagnostics,
					)
				}
				for _, warning := range test.wantWarnings {
					if !strings.Contains(
						diagnostics.String(),
						warning,
					) || strings.Contains(
						output.String(),
						"Warning:",
					) {
						t.Errorf(
							"warning %q not confined to stderr: stdout=%q, stderr=%q",
							warning,
							&output,
							&diagnostics,
						)
					}
				}
				colored := test.forceColor && !asJSON && !test.noColor
				if strings.Contains(
					output.String(),
					"\x1b[",
				) != colored {
					t.Errorf(
						"ANSI styling presence differs from color request: %q",
						&output,
					)
				}
				if strings.ContainsAny(
					output.String(),
					"\x07\r",
				) || strings.Contains(
					output.String(),
					"\x1b]",
				) {
					t.Errorf(
						"untrusted controls reached terminal: %q",
						&output,
					)
				}
				if !asJSON {
					for _, header := range []string{
						"Skill",
						"Description",
						"Model invocation",
						"Location",
					} {
						if !strings.Contains(
							output.String(),
							header,
						) {
							t.Errorf(
								"missing inventory column %q: %q",
								header,
								&output,
							)
						}
					}
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
				for _, text := range test.wantText {
					if !strings.Contains(
						output.String(),
						text,
					) {
						t.Errorf(
							"missing readable text %q: %q",
							text,
							&output,
						)
					}
				}
				if asJSON && !json.Valid(output.Bytes()) {
					t.Errorf(
						"invalid JSON: %q",
						&output,
					)
				}
				if asJSON && len(test.wantWarnings) == 0 && strings.Contains(
					output.String(),
					`"Violations"`,
				) {
					t.Errorf(
						"JSON includes an empty violations field: %q",
						&output,
					)
				}
				if test.wantLineCount > 0 {
					if lines := strings.Split(
						output.String(),
						"\n",
					); len(lines) != test.wantLineCount {
						t.Errorf(
							"listing has %d lines, want %d without outer description whitespace: %q",
							len(lines),
							test.wantLineCount,
							&output,
						)
					}
				}
				if asJSON {
					var entries []map[string]any
					if err := json.Unmarshal(
						output.Bytes(),
						&entries,
					); err != nil {
						t.Fatal(err)
					}
					if len(entries) != len(test.want) {
						t.Fatalf(
							"JSON inventory = %#v, want %d skills",
							entries,
							len(test.want),
						)
					}
					for i, entry := range entries {
						name := test.want[i]
						if len(entry) != 4 || entry["name"] != name {
							t.Fatalf(
								"JSON summary has unexpected fields or name: %#v",
								entry,
							)
						}
						wantDescription := "Alpha skill"
						wantPath := filepath.Join(
							skills.ProjectPath(cwd),
							projectDirectory,
							"SKILL.md",
						)
						switch name {
						case "beta":
							wantDescription = "Beta skill"
							wantPath = filepath.FromSlash("/test-home/.agents/skills/beta/SKILL.md")
						case "gamma":
							wantDescription = "Gamma"
							wantPath = filepath.FromSlash("/custom/gamma/SKILL.md")
						case "delta":
							wantDescription = "Delta\x1b]52;c;evil\a"
							wantPath = sourceDirectory + "/SKILL.md"
						}
						if test.description != "" && (name == "alpha" || name == "delta") {
							wantDescription = test.description
						}
						disabled := test.invocation != nil && *test.invocation && (name == "alpha" || name == "delta")
						if entry["description"] != wantDescription || entry["path"] != wantPath ||
							entry["disable-model-invocation"] != disabled {
							t.Errorf(
								"JSON summary = %#v, want unchanged description %q, path %q, disabled=%t",
								entry,
								wantDescription,
								wantPath,
								disabled,
							)
						}
					}
				}
			},
		)
	}
}

func TestRemove(t *testing.T) {
	outputErr := errors.New("output unavailable")
	for _, test := range []struct {
		name              string
		scope             skills.ScopeOptions
		both, json, all   bool
		nonTTY, pipe      bool
		files             fstest.MapFS
		removed           []string
		summaryNames      map[string]string
		damaged, empty    bool
		fault             managerFSFault
		committed         bool
		outputErr         error
		invalidCrossScope bool
		forceColor        bool
		wantText          []string
		errText           string
	}{
		{
			name: "unambiguous default selection",
		},
		{
			name:   "non-terminal removal automatically receives a summary array",
			nonTTY: true,
		},
		{
			name:       "pipe removal remains JSON with forced color",
			nonTTY:     true,
			pipe:       true,
			forceColor: true,
		},
		{
			name:    "damaged definitions remain removable with an empty description",
			json:    true,
			damaged: true,
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte("invalid"),
				},
			},
		},
		{
			name: "cleanup failure emits committed removal JSON",
			scope: skills.ScopeOptions{
				Project: true,
			},
			nonTTY:    true,
			fault:     managerStageCleanupFailure,
			committed: true,
		},
		{
			name: "JSON writer failure preserves its cause after removal",
			scope: skills.ScopeOptions{
				Project: true,
			},
			nonTTY:    true,
			outputErr: outputErr,
		},
		{
			name: "empty bulk removal produces an empty JSON array",
			scope: skills.ScopeOptions{
				Project: true,
			},
			all:    true,
			nonTTY: true,
			empty:  true,
			files:  fstest.MapFS{},
		},
		{
			name: "all removes the actual mismatched directory",
			all:  true,
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			removed: []string{
				"template",
			},
		},
		{
			name: "all removes every directory despite duplicate declared names",
			all:  true,
			json: true,
			summaryNames: map[string]string{
				"template": "alpha",
			},
			files: fstest.MapFS{
				"alpha/SKILL.md": {
					Data: []byte(alphaSkill),
				},
				"template/SKILL.md": {
					Data: []byte(alphaSkill),
				},
			},
			removed: []string{
				"alpha",
				"template",
			},
		},
		{
			name:       "colored removal",
			forceColor: true,
		},
		{
			name: "colored removal strips directory controls",
			scope: skills.ScopeOptions{
				Directory: new("/custom\x1b]52;c;evil\x07"),
			},
			forceColor: true,
			wantText: []string{
				"/custom/alpha",
			},
		},
		{
			name: "removal paths preserve quotes backslashes and spaces",
			scope: skills.ScopeOptions{
				Directory: new("/custom \"quoted\" \\folder "),
			},
			wantText: []string{
				"/custom \"quoted\" \\folder /alpha",
			},
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
				t.Setenv(
					"CLICOLOR_FORCE",
					"",
				)
				t.Setenv(
					"NO_COLOR",
					"",
				)
				if test.forceColor {
					t.Setenv(
						"CLICOLOR_FORCE",
						"1",
					)
				}
				project := skills.ProjectPath(cwd)
				global := filepath.FromSlash("/test-home/.agents/skills")
				files := test.files
				if files == nil {
					files = fstest.MapFS{
						"alpha/SKILL.md": {
							Data: []byte(alphaSkill),
						},
					}
				}
				destinations := newManagerFilesystem(
					t,
					project,
					files,
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
						filepath.Join(
							*test.scope.Directory,
							"alpha",
						),
						0o700,
					); err != nil {
						t.Fatal(err)
					}
					if err := afero.WriteFile(
						destinations,
						filepath.Join(
							*test.scope.Directory,
							"alpha",
							"SKILL.md",
						),
						[]byte(alphaSkill),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
				}
				names := []string{
					"alpha",
				}
				if test.all {
					names = nil
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
				removalDestinations := destinations
				if test.fault != managerNoFault {
					removalDestinations = managerFaultFS{
						Fs:    destinations,
						fault: test.fault,
					}
				}
				var output bytes.Buffer
				var resultOutput io.Writer = &output
				if test.outputErr != nil {
					resultOutput = commandOutputFailure{
						err: test.outputErr,
					}
				}
				if !test.forceColor {
					t.Setenv(
						"NO_COLOR",
						"1",
					)
				}
				if !test.nonTTY && test.errText == "" {
					resultOutput = commandTerminal(
						t,
						resultOutput,
					)
				} else if test.pipe {
					resultOutput = commandPipe(
						t,
						resultOutput,
					)
				}
				asJSON := test.json || test.nonTTY
				err = skills.Remove(
					t.Context(),
					skills.NewManager(
						nil,
						removalDestinations,
					),
					skills.RemoveOptions{
						Scope:  test.scope,
						Skills: names,
						All:    test.all,
						JSON:   test.json,
					},
					skills.IO{
						Out: resultOutput,
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
				if test.committed {
					if _, ok := errors.AsType[*skills.CommittedCleanupError](err); !ok || !errors.Is(
						err,
						fs.ErrPermission,
					) {
						t.Fatalf(
							"error = %v, want committed cleanup failure",
							err,
						)
					}
				} else if test.outputErr != nil {
					if !errors.Is(
						err,
						test.outputErr,
					) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				selected := project
				if test.scope.Directory != nil {
					selected = *test.scope.Directory
				}
				removed := test.removed
				if test.empty {
					removed = []string{}
					if strings.TrimSpace(output.String()) != "[]" {
						t.Fatalf(
							"empty removal output = %q",
							&output,
						)
					}
				}
				if removed == nil {
					removed = []string{
						"alpha",
					}
				}
				for _, name := range removed {
					if _, err := destinations.Stat(filepath.Join(
						selected,
						name,
					)); !errors.Is(
						err,
						fs.ErrNotExist,
					) {
						t.Errorf(
							"selected bundle %s still exists: %v",
							name,
							err,
						)
					}
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
				if test.outputErr != nil {
					return
				}
				if asJSON {
					var entries []map[string]any
					if err := json.Unmarshal(
						output.Bytes(),
						&entries,
					); err != nil {
						t.Fatal(err)
					}
					if len(entries) != len(removed) {
						t.Fatalf(
							"removed summaries = %#v, want %d skills",
							entries,
							len(removed),
						)
					}
					for i, name := range removed {
						wantName := name
						if declared, ok := test.summaryNames[name]; ok {
							wantName = declared
						}
						description := "Alpha skill"
						if test.damaged {
							description = ""
						}
						entry := entries[i]
						if len(entry) != 3 || entry["name"] != wantName || entry["description"] != description ||
							entry["path"] != filepath.Join(
								selected,
								name,
								"SKILL.md",
							) {
							t.Errorf(
								"removed summary = %#v",
								entry,
							)
						}
					}
				}
				if !asJSON {
					if strings.Contains(
						output.String(),
						"\x1b[",
					) != test.forceColor {
						t.Errorf(
							"unexpected styling in removal output: %q",
							&output,
						)
					}
					for _, text := range append(
						[]string{
							"Removed",
							"Location",
						},
						removed...,
					) {
						if !strings.Contains(
							output.String(),
							text,
						) {
							t.Errorf(
								"missing removal detail %q: %q",
								text,
								&output,
							)
						}
					}
					for _, text := range test.wantText {
						if !strings.Contains(
							output.String(),
							text,
						) {
							t.Errorf(
								"missing readable removal path %q: %q",
								text,
								&output,
							)
						}
					}
					if strings.Contains(
						output.String(),
						"\x1b]",
					) || strings.ContainsAny(
						output.String(),
						"\x07\r",
					) {
						t.Errorf(
							"directory controls reached terminal: %q",
							&output,
						)
					}
				}
			},
		)
	}
}
