package skills_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"

	"github.com/spachava753/cpe/internal/skills"
)

// These properties follow https://agentskills.io/specification; discovery is
// limited to immediate skill directories, as documented in README.md.
// The oracle is the generated domain value, not another frontmatter parser.
func TestLoadValidSkills(t *testing.T) {
	rapid.Check(
		t,
		func(t *rapid.T) {
			want := rapid.MapOfNValues(
				skillGenerator(),
				1,
				3,
				func(skill skills.Skill) string {
					return skill.Name
				},
			).Draw(
				t,
				"skills",
			)
			body := rapid.StringN(
				0,
				2048,
				-1,
			).Draw(
				t,
				"body",
			)
			lineEnding := rapid.SampledFrom([]string{
				"\n",
				"\r\n",
			}).Draw(
				t,
				"line ending",
			)
			finalNewline := rapid.Bool().Draw(
				t,
				"final newline",
			)

			for _, style := range []yaml.Style{
				yaml.DoubleQuotedStyle,
				yaml.SingleQuotedStyle,
				yaml.LiteralStyle,
				yaml.FoldedStyle,
			} {
				for _, withResources := range []bool{
					false,
					true,
				} {
					fsys := make(fstest.MapFS)
					for _, skill := range want {
						content, err := encodeSkill(
							skill,
							style,
						)
						if err != nil {
							t.Fatalf(
								"encode fixture: %v",
								err,
							)
						}
						content = strings.ReplaceAll(
							content,
							"\n",
							lineEnding,
						)
						if !withResources && !finalNewline {
							content = strings.TrimSuffix(
								content,
								lineEnding,
							)
						}
						if withResources {
							content += body
							// Bundled resources are not independently discovered skills.
							fsys[skill.Name+"/references/nested/SKILL.md"] = &fstest.MapFile{
								Data: []byte("invalid frontmatter"),
							}
							fsys[skill.Name+"/scripts/run.sh"] = &fstest.MapFile{
								Data: []byte(body),
							}
						}
						fsys[skill.Path] = &fstest.MapFile{
							Data: []byte(content),
						}
					}
					if withResources {
						fsys["README.md"] = &fstest.MapFile{
							Data: []byte(body),
						}
					}

					got, err := skills.Load(fsys)
					if err != nil {
						for _, name := range slices.Sorted(maps.Keys(want)) {
							path := want[name].Path
							t.Logf(
								"%s:\n%s",
								path,
								fsys[path].Data,
							)
						}
						t.Fatalf(
							"Load rejected valid skills (style=%d, resources=%t): %v",
							style,
							withResources,
							err,
						)
					}
					if len(got) != len(want) {
						t.Fatalf(
							"Load returned %d skills, want %d (style=%d, resources=%t)",
							len(got),
							len(want),
							style,
							withResources,
						)
					}
					seen := make(
						map[string]bool,
						len(got),
					)
					for _, actual := range got {
						expected, exists := want[actual.Name]
						if !exists || seen[actual.Name] {
							t.Fatalf(
								"Load returned unexpected or duplicate skill %q",
								actual.Name,
							)
						}
						seen[actual.Name] = true
						if actual.Name != expected.Name || actual.Description != expected.Description ||
							actual.License != expected.License || actual.Compatibility != expected.Compatibility ||
							actual.InvocationDisabled != expected.InvocationDisabled || actual.Path != expected.Path || len(actual.Violations) != 0 ||
							!cmp.Equal(
								actual.Metadata,
								expected.Metadata,
							) || !slices.Equal(
							actual.AllowedTools,
							expected.AllowedTools,
						) {
							t.Fatalf(
								"Load (style=%d, resources=%t) = %#v, want %#v\nSKILL.md:\n%s",
								style,
								withResources,
								actual,
								expected,
								fsys[expected.Path].Data,
							)
						}
					}
				}
			}
		},
	)
}

// Start with a valid skill and break one constraint. Unlike arbitrary bytes,
// these inputs reach validation rather than mostly failing YAML parsing.
func TestLoadInvalidSkills(t *testing.T) {
	for _, change := range []struct {
		name, field, value string
	}{
		{
			name:  "missing name",
			field: "name",
		},
		{
			name:  "empty name",
			field: "name",
			value: `""`,
		},
		{
			name:  "uppercase name",
			field: "name",
			value: `"A"`,
		},
		{
			name:  "uppercase Unicode name",
			field: "name",
			value: `"É"`,
		},
		{
			name:  "leading hyphen",
			field: "name",
			value: `"-a"`,
		},
		{
			name:  "trailing hyphen",
			field: "name",
			value: `"a-"`,
		},
		{
			name:  "consecutive hyphens",
			field: "name",
			value: `"a--b"`,
		},
		{
			name:  "underscore in name",
			field: "name",
			value: `"a_b"`,
		},
		{
			name:  "space in name",
			field: "name",
			value: `"a b"`,
		},
		{
			name:  "path separator in name",
			field: "name",
			value: `"a/b"`,
		},
		{
			name:  "non-alphanumeric Unicode name",
			field: "name",
			value: `"a🙂"`,
		},
		{
			name:  "non-string name",
			field: "name",
			value: "[a]",
		},
		{
			name:  "numeric name",
			field: "name",
			value: "123",
		},
		{
			name:  "boolean name",
			field: "name",
			value: "true",
		},
		{
			name:  "null name",
			field: "name",
			value: "null",
		},
		{
			name:  "missing description",
			field: "description",
		},
		{
			name:  "empty description",
			field: "description",
			value: `""`,
		},
		{
			name:  "blank description",
			field: "description",
			value: `" \t\n "`,
		},
		{
			name:  "non-string description",
			field: "description",
			value: "[text]",
		},
		{
			name:  "numeric description",
			field: "description",
			value: "123",
		},
		{
			name:  "boolean description",
			field: "description",
			value: "true",
		},
		{
			name:  "null description",
			field: "description",
			value: "null",
		},
		{
			name:  "empty compatibility",
			field: "compatibility",
			value: `""`,
		},
		{
			name:  "501 character compatibility",
			field: "compatibility",
			value: strconv.Quote(strings.Repeat(
				"中",
				501,
			)),
		},
		{
			name:  "non-string compatibility",
			field: "compatibility",
			value: "[git]",
		},
		{
			name:  "null compatibility",
			field: "compatibility",
			value: "null",
		},
		{
			name:  "non-string license",
			field: "license",
			value: "[MIT]",
		},
		{
			name:  "null license",
			field: "license",
			value: "null",
		},
		{
			name:  "duplicate metadata key",
			field: "metadata",
			value: "\n  author: one\n  author: two",
		},
		{
			name:  "non-string allowed tools",
			field: "allowed-tools",
			value: "[Read]",
		},
		{
			name:  "null allowed tools",
			field: "allowed-tools",
			value: "null",
		},
		{
			name:  "non-boolean invocation control",
			field: "disable-model-invocation",
			value: `"true"`,
		},
		{
			name:  "null invocation control",
			field: "disable-model-invocation",
			value: "null",
		},
		{
			name: "missing opening delimiter",
		},
		{
			name: "missing closing delimiter",
		},
		{
			name: "non-map frontmatter",
		},
		{
			name: "malformed YAML",
		},
		{
			name: "duplicate name",
		},
		{
			name: "duplicate aliased field",
		},
		{
			name: "trailing YAML data",
		},
	} {
		t.Run(
			change.name,
			rapid.MakeCheck(func(t *rapid.T) {
				name := nameGenerator().Draw(
					t,
					"name",
				)
				description := textGenerator(1024).Draw(
					t,
					"description",
				)
				fields := []string{
					"name: " + strconv.Quote(name),
					"description: " + strconv.Quote(description),
				}
				value := change.value
				if change.field != "" {
					fields = slices.DeleteFunc(
						fields,
						func(field string) bool {
							return strings.HasPrefix(
								field,
								change.field+":",
							)
						},
					)
					if value != "" {
						fields = append(
							fields,
							change.field+": "+value,
						)
					}
				}
				content := "---\n" + strings.Join(
					fields,
					"\n",
				) + "\n---\n"
				switch change.name {
				case "missing opening delimiter":
					content = strings.TrimPrefix(
						content,
						"---\n",
					)
				case "missing closing delimiter":
					content = strings.TrimSuffix(
						content,
						"---\n",
					)
				case "non-map frontmatter":
					content = "---\n- name: example\n---\n"
				case "malformed YAML":
					content = "---\nname: [\n---\n"
				case "duplicate name":
					content = strings.Replace(
						content,
						"\n---\n",
						"\nname: "+strconv.Quote(name)+"\n---\n",
						1,
					)
				case "duplicate aliased field":
					content = strings.Replace(
						content,
						"\n---\n",
						"\nlicense: &field \"license\"\n? *field\n: \"duplicate\"\n---\n",
						1,
					)
				case "trailing YAML data":
					content = strings.Replace(
						content,
						"\n---\n",
						"\n...\nextra: document\n---\n",
						1,
					)
				}

				directory := name
				if change.field == "name" {
					invalid, err := strconv.Unquote(value)
					if err != nil {
						invalid = value
					}
					if invalid != "" && !strings.Contains(
						invalid,
						"/",
					) {
						directory = invalid
					}
				}
				path := directory + "/SKILL.md"
				if _, err := skills.Load(fstest.MapFS{
					path: &fstest.MapFile{
						Data: []byte(content),
					},
				}); err == nil {
					t.Fatalf(
						"Load accepted %s at %s:\n%s",
						change.name,
						path,
						content,
					)
				}
			}),
		)
	}
}

// Keep coverage-guided fuzzing for malformed encodings and YAML constructs
// outside the structured generators' subset.
func FuzzLoadArbitraryFiles(f *testing.F) {
	for _, content := range []string{
		"",
		"---",
		"\xff\x00",
		"---\n[]\n---\n",
		"---\nname: example\ndescription: valid\n---\n",
		"---\nname: example\ndescription: \"embedded --- delimiter\"\n---\n",
		"---\nname: &name example\ndescription: *name\n---\n",
	} {
		f.Add([]byte(content))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		got, err := skills.Load(fstest.MapFS{
			"example/SKILL.md": &fstest.MapFile{
				Data: content,
			},
		})
		if err != nil {
			return
		}
		if len(got) != 1 {
			t.Fatalf(
				"successful Load returned %d skills, want 1",
				len(got),
			)
		}
		skill := got[0]
		if !utf8.ValidString(skill.Name) || strings.TrimSpace(skill.Name) == "" || skill.Path != "example/SKILL.md" ||
			!utf8.ValidString(skill.Description) || strings.TrimSpace(skill.Description) == "" ||
			!utf8.ValidString(skill.Compatibility) || utf8.RuneCountInString(skill.Compatibility) > 500 ||
			((skill.Name != "example" || utf8.RuneCountInString(skill.Name) > 64 ||
				utf8.RuneCountInString(skill.Description) > 1024) != (len(skill.Violations) > 0)) {
			t.Fatalf(
				"successful Load returned invalid skill: %#v",
				skill,
			)
		}
	})
}

func TestLoadFilesystem(t *testing.T) {
	t.Parallel()
	valid := func(name string) *fstest.MapFile {
		return &fstest.MapFile{
			Data: []byte("---\nname: " + name + "\ndescription: valid\n---\n"),
		}
	}
	for _, test := range []struct {
		wantCause   error
		files       fstest.MapFS
		faults      map[string]error
		metadata    map[string]any
		name        string
		unopened    string
		description string
		skillName   string
		violations  []string
		tools       []string
		wantCount   int
		wantError   bool
	}{
		{
			name:  "empty root",
			files: fstest.MapFS{},
		},
		{
			name: "ignores root files",
			files: fstest.MapFS{
				"README.md": {
					Data: []byte("not a skill"),
				},
			},
		},
		{
			name: "missing skill file",
			files: fstest.MapFS{
				"example": {
					Mode: fs.ModeDir,
				},
			},
			wantError: true,
			wantCause: fs.ErrNotExist,
		},
		{
			name: "broken symlink",
			files: fstest.MapFS{
				"example": {
					Mode: fs.ModeSymlink,
					Data: []byte("missing"),
				},
			},
			wantError: true,
			wantCause: fs.ErrNotExist,
		},
		{
			name: "symlink to regular file",
			files: fstest.MapFS{
				"link": {
					Mode: fs.ModeSymlink,
					Data: []byte("README.md"),
				},
				"README.md": {},
			},
		},
		{
			name: "directory mismatch loads declared name with a violation",
			files: fstest.MapFS{
				"template/SKILL.md": valid("template-skill"),
			},
			wantCount: 1,
			skillName: "template-skill",
			violations: []string{
				`name "template-skill" does not match directory "template"`,
			},
		},
		{
			name: "overlong description loads unchanged with a violation",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: " + strings.Repeat(
						"é",
						1025,
					) + "\n---\n"),
				},
			},
			wantCount: 1,
			description: strings.Repeat(
				"é",
				1025,
			),
			violations: []string{
				"description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name: "overlong Unicode name loads unchanged with a violation",
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
			wantCount: 1,
			skillName: strings.Repeat(
				"é",
				65,
			),
			violations: []string{
				"name exceeds 64 characters (found 65)",
			},
		},
		{
			name: "both soft limits record violations in field order",
			files: fstest.MapFS{
				strings.Repeat(
					"a",
					65,
				) + "/SKILL.md": {
					Data: []byte("---\nname: " + strings.Repeat(
						"a",
						65,
					) + "\ndescription: " + strings.Repeat(
						"é",
						1025,
					) + "\n---\n"),
				},
			},
			wantCount: 1,
			skillName: strings.Repeat(
				"a",
				65,
			),
			description: strings.Repeat(
				"é",
				1025,
			),
			violations: []string{
				"name exceeds 64 characters (found 65)",
				"description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name: "directory mismatch and both length violations are preserved",
			files: fstest.MapFS{
				"template/SKILL.md": {
					Data: []byte("---\nname: " + strings.Repeat(
						"a",
						65,
					) + "\ndescription: " + strings.Repeat(
						"é",
						1025,
					) + "\n---\n"),
				},
			},
			wantCount: 1,
			skillName: strings.Repeat(
				"a",
				65,
			),
			description: strings.Repeat(
				"é",
				1025,
			),
			violations: []string{
				`name "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" does not match directory "template"`,
				"name exceeds 64 characters (found 65)",
				"description exceeds 1024 characters (found 1025)",
			},
		},
		{
			name: "Unicode character boundaries produce no violations",
			files: fstest.MapFS{
				strings.Repeat(
					"é",
					64,
				) + "/SKILL.md": {
					Data: []byte("---\nname: " + strings.Repeat(
						"é",
						64,
					) + "\ndescription: " + strings.Repeat(
						"🙂",
						1024,
					) + "\n---\n"),
				},
			},
			wantCount: 1,
		},
		{
			name: "ignores unknown string field",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nhomepage: https://browser-use.com\n---\n"),
				},
			},
			wantCount:   1,
			description: "valid",
		},
		{
			name: "ignores unknown scalar and structured fields",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\ncustom-flag: true\ncustom-number: 42\ncustom-null: null\ncustom-list: [Read, 1, false]\ncustom-map:\n  requires:\n    bins: [browser-use]\nmetadata:\n  author: someone\nallowed-tools: Read\n---\n"),
				},
			},
			wantCount:   1,
			description: "valid",
			metadata: map[string]any{
				"author": "someone",
			},
			tools: []string{
				"Read",
			},
		},
		{
			name: "ignores unknown aliased field",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: &field homepage\n? *field\n: https://browser-use.com\n---\n"),
				},
			},
			wantCount:   1,
			description: "homepage",
		},
		{
			name: "preserves nested client metadata",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nhomepage: https://browser-use.com\nmetadata: {author: someone, openclaw: {requires: {bins: [browser-use]}, install: [{id: uv, kind: uv, package: browser-use, bins: [browser-use]}]}}\n---\n"),
				},
			},
			wantCount:   1,
			description: "valid",
			metadata: map[string]any{
				"author": "someone",
				"openclaw": map[string]any{
					"requires": map[string]any{
						"bins": []any{
							"browser-use",
						},
					},
					"install": []any{
						map[string]any{
							"id":      "uv",
							"kind":    "uv",
							"package": "browser-use",
							"bins": []any{
								"browser-use",
							},
						},
					},
				},
			},
		},
		{
			name: "preserves non-string metadata entries",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata:\n  author: someone\n  123: value\n  version: 123\n  tags: [one, two]\n  flag: true\n  empty: null\n---\n"),
				},
			},
			wantCount: 1,
			metadata: map[string]any{
				"author":  "someone",
				"123":     "value",
				"version": 123,
				"tags": []any{
					"one",
					"two",
				},
				"flag":  true,
				"empty": nil,
			},
		},
		{
			name: "numeric root metadata keys retain their spelling",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata:\n  1: original\n  01: separate\n---\n"),
				},
			},
			wantCount: 1,
			metadata: map[string]any{
				"1":  "original",
				"01": "separate",
			},
		},
		{
			name: "duplicate nested numeric metadata key",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata:\n  nested:\n    1: original\n    01: overwritten\n---\n"),
				},
			},
			wantError: true,
		},
		{
			name: "duplicate nested aliased numeric metadata key",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata:\n  nested:\n    1: &key 01\n    ? *key\n    : overwritten\n---\n"),
				},
			},
			wantError: true,
		},
		{
			name: "duplicate aliased metadata key",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata:\n  author: &key author\n  ? *key\n  : overwritten\n---\n"),
				},
			},
			wantError: true,
		},
		{
			name: "non-map metadata",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata: author\n---\n"),
				},
			},
			wantError: true,
		},
		{
			name: "null metadata is absent",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata: null\n---\n"),
				},
			},
			wantCount: 1,
			metadata:  map[string]any{},
		},
		{
			name: "YAML aliases",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: &name example\ndescription: *name\nmetadata:\n  original: &value text\n  copy: *value\n---\n"),
				},
			},
			wantCount:   1,
			description: "example",
			metadata: map[string]any{
				"original": "text",
				"copy":     "text",
			},
		},
		{
			name: "tool whitespace",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nallowed-tools: \"Read  Bash(git:*)\\tRead\"\n---\n"),
				},
			},
			wantCount: 1,
			tools: []string{
				"Read",
				"Bash(git:*)",
				"Read",
			},
		},
		{
			name: "empty optional collections",
			files: fstest.MapFS{
				"example/SKILL.md": {
					Data: []byte("---\nname: example\ndescription: valid\nmetadata: {}\nallowed-tools: \"\"\n---\n"),
				},
			},
			wantCount: 1,
			metadata:  map[string]any{},
			tools:     []string{},
		},
		{
			name: "multiple skills",
			files: fstest.MapFS{
				"a/SKILL.md": valid("a"),
				"b/SKILL.md": valid("b"),
			},
			wantCount: 2,
		},
		{
			name:  "root read error",
			files: fstest.MapFS{},
			faults: map[string]error{
				"readdir .": fs.ErrPermission,
			},
			wantError: true,
			wantCause: fs.ErrPermission,
		},
		{
			name: "skill open error",
			files: fstest.MapFS{
				"a/SKILL.md": valid("a"),
				"b/SKILL.md": valid("b"),
			},
			faults: map[string]error{
				"open a/SKILL.md": fs.ErrPermission,
			},
			wantError: true,
			wantCause: fs.ErrPermission,
			unopened:  "b/SKILL.md",
		},
		{
			name: "skill read error",
			files: fstest.MapFS{
				"a/SKILL.md": valid("a"),
				"b/SKILL.md": valid("b"),
			},
			faults: map[string]error{
				"read a/SKILL.md": fs.ErrPermission,
			},
			wantError: true,
			wantCause: fs.ErrPermission,
			unopened:  "b/SKILL.md",
		},
		{
			name: "stops at first invalid skill",
			files: fstest.MapFS{
				"a/SKILL.md": {
					Data: []byte("invalid"),
				},
				"b/SKILL.md": valid("b"),
			},
			wantError: true,
			unopened:  "b/SKILL.md",
		},
		{
			name: "stops after valid prefix",
			files: fstest.MapFS{
				"a/SKILL.md": valid("a"),
				"b/SKILL.md": {
					Data: []byte("invalid"),
				},
				"c/SKILL.md": valid("c"),
			},
			wantError: true,
			unopened:  "c/SKILL.md",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				t.Parallel()
				fsys := &observedSkillFS{
					ReadDirFS: test.files,
					faults:    test.faults,
				}
				got, err := skills.Load(fsys)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"Load error = %v, want error=%t",
						err,
						test.wantError,
					)
				}
				if test.wantCause != nil && !errors.Is(
					err,
					test.wantCause,
				) {
					t.Fatalf(
						"Load error = %v, want cause %v",
						err,
						test.wantCause,
					)
				}
				if !test.wantError && len(got) != test.wantCount {
					t.Fatalf(
						"Load returned %d skills, want %d",
						len(got),
						test.wantCount,
					)
				}
				if test.unopened != "" && slices.Contains(
					fsys.calls,
					"open "+test.unopened,
				) {
					t.Fatalf(
						"Load continued past the first invalid skill: %v",
						fsys.calls,
					)
				}
				if test.wantError && len(got) != 0 {
					t.Fatalf(
						"Load returned partial results on error: %#v",
						got,
					)
				}
				if test.description != "" && got[0].Description != test.description {
					t.Fatalf(
						"description = %q, want %q",
						got[0].Description,
						test.description,
					)
				}
				if test.skillName != "" && got[0].Name != test.skillName {
					t.Fatalf(
						"name = %q, want unchanged %q",
						got[0].Name,
						test.skillName,
					)
				}
				if len(got) > 0 {
					if diff := cmp.Diff(
						test.violations,
						got[0].Violations,
						cmpopts.EquateEmpty(),
					); diff != "" {
						t.Fatalf(
							"violations (-want +got):\n%s",
							diff,
						)
					}
				}
				if test.metadata != nil {
					if diff := cmp.Diff(
						test.metadata,
						got[0].Metadata,
						cmpopts.EquateEmpty(),
					); diff != "" {
						t.Fatalf(
							"metadata (-want +got):\n%s",
							diff,
						)
					}
				}
				if test.tools != nil && !slices.Equal(
					got[0].AllowedTools,
					test.tools,
				) {
					t.Fatalf(
						"tools = %#v, want %#v",
						got[0].AllowedTools,
						test.tools,
					)
				}
			},
		)
	}
	t.Run(
		"symlinked skill directory",
		func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			catalog := filepath.Join(
				root,
				"catalog",
			)
			source := filepath.Join(
				root,
				"source",
			)
			for _, dir := range []string{
				catalog,
				source,
			} {
				if err := os.Mkdir(
					dir,
					0o700,
				); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(
				filepath.Join(
					source,
					"SKILL.md",
				),
				valid("example").Data,
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(
				source,
				filepath.Join(
					catalog,
					"example",
				),
			); err != nil {
				t.Fatal(err)
			}
			got, err := skills.Load(os.DirFS(catalog).(fs.ReadDirFS))
			if err != nil || len(got) != 1 || got[0].Name != "example" || got[0].Path != "example/SKILL.md" {
				t.Fatalf(
					"Load symlink = %#v, %v",
					got,
					err,
				)
			}
		},
	)
}

func nameGenerator() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.StringMatching(`^[a-z0-9éλ中１٢]([a-z0-9éλ中１٢-]{0,62}[a-z0-9éλ中１٢])?$`).Filter(func(name string) bool {
			return !strings.Contains(
				name,
				"--",
			)
		}),
		rapid.StringOfN(
			rapid.SampledFrom([]rune("aé中1")),
			63,
			64,
			-1,
		),
	)
}

// Swarm small alphabets; explicitly include the upper limits instead of hoping
// a general string generator reaches them. Limits count runes, not bytes.
func textGenerator(limit int) *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		alphabet := rapid.SampledFrom([]string{
			"ab",
			"é中🙂",
			"a:#'\"\\",
			"a\n\tb",
			"a---b",
		}).Draw(
			t,
			"alphabet",
		)
		runes := rapid.SampledFrom([]rune(alphabet))
		return rapid.OneOf(
			rapid.Just("embedded --- delimiter"),
			rapid.Just("1.0"),
			rapid.StringOfN(
				runes,
				1,
				min(
					32,
					limit,
				),
				-1,
			),
			rapid.StringOfN(
				runes,
				limit-1,
				limit,
				-1,
			),
		).Filter(func(text string) bool { return strings.TrimSpace(text) == text }).Draw(
			t,
			"text",
		)
	})
}

func skillGenerator() *rapid.Generator[skills.Skill] {
	return rapid.Custom(func(t *rapid.T) skills.Skill {
		skill := skills.Skill{
			Name: nameGenerator().Draw(
				t,
				"name",
			),
			Description: textGenerator(1024).Draw(
				t,
				"description",
			),
			InvocationDisabled: rapid.Bool().Draw(
				t,
				"disable-model-invocation",
			),
		}
		if rapid.Bool().Draw(
			t,
			"has license",
		) {
			skill.License = textGenerator(32).Draw(
				t,
				"license",
			)
		}
		if rapid.Bool().Draw(
			t,
			"has compatibility",
		) {
			skill.Compatibility = textGenerator(500).Draw(
				t,
				"compatibility",
			)
		}
		if rapid.Bool().Draw(
			t,
			"has metadata",
		) {
			skill.Metadata = rapid.MapOfN(
				rapid.SampledFrom([]string{
					"author",
					"version",
					"cpe:key",
				}),
				rapid.Map(
					textGenerator(32),
					func(value string) any { return value },
				),
				1,
				3,
			).Draw(
				t,
				"metadata",
			)
		}
		if rapid.Bool().Draw(
			t,
			"has allowed tools",
		) {
			skill.AllowedTools = rapid.SliceOfN(
				rapid.SampledFrom([]string{
					"Read",
					"Bash(git:*)",
					"Bash(jq:*)",
					"Edit",
				}),
				1,
				4,
			).Draw(
				t,
				"allowed-tools",
			)
		}
		skill.Path = skill.Name + "/SKILL.md"
		return skill
	})
}

func encodeSkill(skill skills.Skill, style yaml.Style) (string, error) {
	scalar := func(value string) *yaml.Node {
		scalarStyle := style
		// Folded emission can change newlines before tab-indented content.
		if style == yaml.FoldedStyle && strings.ContainsAny(
			value,
			"\n\t",
		) {
			scalarStyle = yaml.LiteralStyle
		}
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: value,
			Style: scalarStyle,
		}
	}
	fields := map[string]any{
		"name":        scalar(skill.Name),
		"description": scalar(skill.Description),
	}
	for key, value := range map[string]string{
		"license":       skill.License,
		"compatibility": skill.Compatibility,
		"allowed-tools": strings.Join(
			skill.AllowedTools,
			" ",
		),
	} {
		if value != "" {
			fields[key] = scalar(value)
		}
	}
	if skill.Metadata != nil {
		fields["metadata"] = skill.Metadata
	}
	// Exercise both omitted and explicit false, as well as true.
	if skill.InvocationDisabled || style != yaml.DoubleQuotedStyle {
		fields["disable-model-invocation"] = skill.InvocationDisabled
	}
	content, err := yaml.Marshal(fields)
	if err != nil {
		return "", err
	}
	return "---\n" + string(content) + "---\n", nil
}

// Record file access and inject errors without touching the host filesystem.
type observedSkillFS struct {
	fs.ReadDirFS
	faults map[string]error
	calls  []string
}

func (fsys *observedSkillFS) ReadDir(name string) ([]fs.DirEntry, error) {
	fsys.calls = append(
		fsys.calls,
		"readdir "+name,
	)
	if err := fsys.faults["readdir "+name]; err != nil {
		return nil, err
	}
	return fsys.ReadDirFS.ReadDir(name)
}

func (fsys *observedSkillFS) Open(name string) (fs.File, error) {
	fsys.calls = append(
		fsys.calls,
		"open "+name,
	)
	if err := fsys.faults["open "+name]; err != nil {
		return nil, err
	}
	file, err := fsys.ReadDirFS.Open(name)
	if err != nil {
		return nil, err
	}
	if readErr := fsys.faults["read "+name]; readErr != nil {
		file = &unreadableSkillFile{
			File: file,
			err:  readErr,
		}
	}
	return file, nil
}

type unreadableSkillFile struct {
	fs.File
	err error
}

func (file *unreadableSkillFile) Read([]byte) (int, error) {
	return 0, file.err
}
