package skills_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
				if test.json {
					if !json.Valid(output.Bytes()) {
						t.Fatalf(
							"invalid JSON: %q",
							&output,
						)
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
		want              []string
		unsafeDescription bool
		directoryMismatch bool
		description       string
		wantText          []string
		wantLineCount     int
		wantWarnings      []string
		violations        []string
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
			name:              "installed JSON retains a directory mismatch violation",
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
			violations: []string{
				`name "alpha" does not match directory "template"`,
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
			name:              "source JSON retains a directory mismatch violation",
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
			violations: []string{
				`name "delta" does not match directory "template"`,
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
			description: " Alpha \x1b[2J\x1b[H\x1b[31mred\x1b[0m \x1b]8;;https://evil.invalid\x1b\\link\x1b]8;;\x1b\\ \x1b]52;c;ZXZpbA==\a safe\a\r\b\x00\v\f\x7f\u009bEND \n",
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
			description: "Delta\x1b[2J\x1b[H\x1b]52;c;ZXZpbA==\a safe\r\b\x00\u009bEND",
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
			name: "JSON inventory retains violations with stderr warnings",
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
			violations: []string{
				"description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name:   "source JSON retains violations with stderr warnings",
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
			violations: []string{
				"description exceeds 1024 characters (found 1025)",
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
				if test.noColor {
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
				projectDirectory := "alpha"
				sourceDirectory := "delta"
				if test.directoryMismatch {
					projectDirectory = "template"
					sourceDirectory = "template"
				}
				destinations := newManagerFilesystem(
					t,
					skills.ProjectPath(cwd),
					fstest.MapFS{
						projectDirectory + "/SKILL.md": {
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
				sourceDefinition := "---\nname: delta\ndescription: \"Delta\\e]52;c;evil\\a\"\n---\n"
				if test.description != "" {
					sourceDefinition = "---\nname: delta\ndescription: " + strconv.Quote(test.description) + "\n---\n"
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
						Out: &output,
						Err: warningOutput,
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
				colored := test.forceColor && !test.json && !test.noColor
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
				if !test.json {
					for _, header := range []string{
						"Skill",
						"Description",
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
				if test.json && !json.Valid(output.Bytes()) {
					t.Errorf(
						"invalid JSON: %q",
						&output,
					)
				}
				if test.json && len(test.wantWarnings) == 0 && strings.Contains(
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
				if test.description != "" && test.json {
					var description string
					var violations []string
					if test.source != nil {
						var entries []skills.Skill
						if err := json.Unmarshal(
							output.Bytes(),
							&entries,
						); err != nil {
							t.Fatal(err)
						}
						if len(entries) != 1 {
							t.Fatalf(
								"source inventory = %#v",
								entries,
							)
						}
						description = entries[0].Description
						violations = entries[0].Violations
					} else {
						var entries []skills.InstalledSkill
						if err := json.Unmarshal(
							output.Bytes(),
							&entries,
						); err != nil {
							t.Fatal(err)
						}
						if len(entries) != 2 {
							t.Fatalf(
								"installed inventory = %#v",
								entries,
							)
						}
						description = entries[0].Skill.Description
						violations = entries[0].Skill.Violations
					}
					if !slices.Equal(
						violations,
						test.violations,
					) {
						t.Errorf(
							"JSON violations = %#v, want %#v",
							violations,
							test.violations,
						)
					}
					if description != test.description {
						t.Errorf(
							"JSON description = %q, want unchanged %q",
							description,
							test.description,
						)
					}
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
		both, json, all   bool
		files             fstest.MapFS
		removed           []string
		invalidCrossScope bool
		forceColor        bool
		wantText          []string
		errText           string
	}{
		{
			name: "unambiguous default selection",
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
						All:    test.all,
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
				removed := test.removed
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
				if test.json {
					var paths []string
					if err := json.Unmarshal(
						output.Bytes(),
						&paths,
					); err != nil {
						t.Fatal(err)
					}
					var wantPaths []string
					for _, name := range removed {
						wantPaths = append(
							wantPaths,
							filepath.Join(
								selected,
								name,
							),
						)
					}
					if diff := cmp.Diff(
						wantPaths,
						paths,
					); diff != "" {
						t.Fatalf(
							"removed paths (-want +got):\n%s",
							diff,
						)
					}
				}
				if !test.json {
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
