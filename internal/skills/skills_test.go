package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	const valid = "name: review\ndescription: Review code changes.\n"
	exactLimit := "---\n" + valid + "#" + strings.Repeat(" ", maxFrontmatter-len(valid)-9) + "\n---"
	for _, tc := range []struct {
		name, source, wantError string
		policy                  invocation
		description             string
		directory               string
	}{
		{name: "minimal", source: "---\n" + valid + "---\nBody is not metadata.", policy: byUser | byModel, description: "Review code changes."},
		{name: "Unicode lowercase name", source: "---\nname: révision\ndescription: Review\n---", directory: "révision", policy: byUser | byModel, description: "Review"},
		{name: "name limit counts characters", source: "---\nname: " + strings.Repeat("é", 64) + "\ndescription: Review\n---", directory: strings.Repeat("é", 64), policy: byUser | byModel, description: "Review"},
		{name: "CRLF and BOM", source: "\uFEFF---\r\nname: review\r\ndescription: Review\r\n---\r\n", policy: byUser | byModel, description: "Review"},
		{name: "folded description and optional metadata", source: "---\nname: review\ndescription: >\n  Review code\n  changes.\nlicense: MIT\nmetadata:\n  author: example\nallowed-tools: Read\n---\n", policy: byUser | byModel, description: "Review code changes."},
		{name: "user only", source: "---\n" + valid + "disable-model-invocation: true\n---", policy: byUser, description: "Review code changes."},
		{name: "model only", source: "---\n" + valid + "user-invocable: false\n---", policy: byModel, description: "Review code changes."},
		{name: "explicit defaults", source: "---\n" + valid + "disable-model-invocation: false\nuser-invocable: true\n---", policy: byUser | byModel, description: "Review code changes."},
		{name: "disabled on both surfaces", source: "---\n" + valid + "disable-model-invocation: true\nuser-invocable: false\n---", description: "Review code changes."},
		{name: "body is not parsed or bounded", source: "---\n" + valid + "---\n" + strings.Repeat("[not yaml", maxFrontmatter), policy: byUser | byModel, description: "Review code changes."},
		{name: "exact frontmatter limit", source: exactLimit, policy: byUser | byModel, description: "Review code changes."},
		{name: "reader limit cannot create a closing delimiter", source: strings.TrimSuffix(exactLimit, "---") + "\n---not-a-delimiter", wantError: "within 64 KiB"},
		{name: "missing frontmatter", source: valid, wantError: "must start"},
		{name: "missing terminator", source: "---\n" + valid, wantError: "must end"},
		{name: "oversized frontmatter", source: "---\n" + valid + strings.Repeat("# comment\n", maxFrontmatter) + "---", wantError: "within 64 KiB"},
		{name: "malformed YAML", source: "---\nname: [\n---", wantError: "parse frontmatter"},
		{name: "duplicate name", source: "---\n" + valid + "name: review\n---", wantError: "already defined"},
		{name: "duplicate policy", source: "---\n" + valid + "disable-model-invocation: true\ndisable-model-invocation: false\n---", wantError: "already defined"},
		{name: "missing name", source: "---\ndescription: Review\n---", wantError: "name must"},
		{name: "uppercase name", source: "---\nname: Review\ndescription: Review\n---", wantError: "lowercase"},
		{name: "numeric name", source: "---\nname: 123\ndescription: Review\n---", wantError: "name must"},
		{name: "long name", source: "---\nname: " + strings.Repeat("a", 65) + "\ndescription: Review\n---", wantError: "name must"},
		{name: "consecutive hyphens", source: "---\nname: re--view\ndescription: Review\n---", wantError: "single hyphens"},
		{name: "leading hyphen", source: "---\nname: -review\ndescription: Review\n---", wantError: "leading or trailing"},
		{name: "trailing hyphen", source: "---\nname: review-\ndescription: Review\n---", wantError: "leading or trailing"},
		{name: "directory mismatch", source: "---\nname: other\ndescription: Review\n---", wantError: "directory name"},
		{name: "missing description", source: "---\nname: review\n---", wantError: "description must"},
		{name: "empty description", source: "---\nname: review\ndescription: '  '\n---", wantError: "description must"},
		{name: "numeric description", source: "---\nname: review\ndescription: 123\n---", wantError: "description must"},
		{name: "long description", source: "---\nname: review\ndescription: " + strings.Repeat("é", 1025) + "\n---", wantError: "1024 characters"},
		{name: "quoted invocation flag", source: "---\n" + valid + "disable-model-invocation: 'true'\n---", wantError: "must be a YAML boolean"},
		{name: "null invocation flag", source: "---\n" + valid + "disable-model-invocation: null\n---", wantError: "must be a YAML boolean"},
		{name: "invalid user flag", source: "---\n" + valid + "user-invocable: [false]\n---", wantError: "must be a YAML boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := tc.directory
			if directory == "" {
				directory = "review"
			}
			path := filepath.Join(t.TempDir(), directory, "SKILL.md")
			got, err := parse(strings.NewReader(tc.source), path)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("parse error = %v; want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.name != directory || got.description != tc.description || got.path != path || got.invocable != tc.policy {
				t.Fatalf("parsed skill = %+v", got)
			}
		})
	}
}

func TestDiscover(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(*testing.T, string, string)
		want     []string
		warnings int
	}{
		{name: "missing roots"},
		{name: "sorted immediate children", setup: func(t *testing.T, global, project string) {
			writeSkill(t, global, "zebra", "description: Zebra\n")
			writeSkill(t, project, "alpha", "description: Alpha\n")
			writeSkill(t, filepath.Join(project, "nested"), "hidden", "description: Hidden\n")
			if err := os.WriteFile(filepath.Join(project, "README.md"), []byte("not a skill"), 0600); err != nil {
				t.Fatal(err)
			}
		}, want: []string{"alpha", "zebra"}},
		{name: "project overrides global policy", setup: func(t *testing.T, global, project string) {
			writeSkill(t, global, "review", "description: Global\n")
			writeSkill(t, project, "review", "description: Project\ndisable-model-invocation: true\n")
		}, want: []string{"review"}, warnings: 1},
		{name: "invalid override does not reveal global", setup: func(t *testing.T, global, project string) {
			writeSkill(t, global, "review", "description: Global\n")
			writeSkill(t, project, "review", "description: Project\ndisable-model-invocation: 'true'\n")
		}, warnings: 2},
		{name: "invalid skill does not block neighbors", setup: func(t *testing.T, global, project string) {
			writeSkill(t, global, "invalid", "description: null\n")
			writeSkill(t, project, "valid", "description: Valid\n")
		}, want: []string{"valid"}, warnings: 1},
		{name: "symlink directory", setup: func(t *testing.T, global, project string) {
			writeSkill(t, global, "review", "description: Linked\n")
			if err := os.MkdirAll(project, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(global, "review"), filepath.Join(project, "review")); err != nil {
				t.Fatal(err)
			}
		}, want: []string{"review"}, warnings: 1},
		{name: "root is a file", setup: func(t *testing.T, global, project string) {
			if err := os.WriteFile(global, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
		}, warnings: 1},
		{name: "SKILL.md is a directory", setup: func(t *testing.T, global, project string) {
			if err := os.MkdirAll(filepath.Join(project, "review", "SKILL.md"), 0700); err != nil {
				t.Fatal(err)
			}
		}, warnings: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			global, project := filepath.Join(dir, "global"), filepath.Join(dir, "project")
			if tc.setup != nil {
				tc.setup(t, global, project)
			}
			catalog, warnings := Discover(global, project)
			var names []string
			for _, entry := range catalog.entries {
				names = append(names, entry.name)
				if !filepath.IsAbs(entry.path) {
					t.Errorf("relative skill path %q", entry.path)
				}
			}
			if !reflect.DeepEqual(names, tc.want) || len(warnings) != tc.warnings {
				t.Fatalf("skills = %v, warnings = %v; want %v, %d warnings", names, warnings, tc.want, tc.warnings)
			}
			if tc.name == "project overrides global policy" {
				if catalog.entries[0].path != filepath.Join(project, "review", "SKILL.md") || catalog.Instructions() != "" {
					t.Fatal("global skill leaked through project override")
				}
			}
		})
	}
}

func TestCatalogCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy invocation
		want   bool
	}{
		{"both", byUser | byModel, true},
		{"user only", byUser, true},
		{"model only", byModel, false},
		{"neither", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := Catalog{entries: []skill{{name: "review", description: "Review code", invocable: tc.policy}}}
			commands := catalog.Commands()
			if !tc.want {
				if len(commands) != 0 {
					t.Fatalf("commands = %v", commands)
				}
				return
			}
			if len(commands) != 1 || commands[0] != (command{Name: "/skill:review", Description: "Review code"}) {
				t.Fatalf("commands = %v", commands)
			}
			commands[0].Name = "changed"
			if catalog.Commands()[0].Name != "/skill:review" {
				t.Fatal("caller mutated catalog")
			}
		})
	}
}

func TestCatalogInstructions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy invocation
		want   bool
	}{
		{"both", byUser | byModel, true},
		{"user only", byUser, false},
		{"model only", byModel, true},
		{"neither", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := Catalog{entries: []skill{{name: "review", description: "Review code", path: "/skills/review/SKILL.md", invocable: tc.policy}}}
			instructions := catalog.Instructions()
			if !tc.want {
				if instructions != "" {
					t.Fatalf("hidden skill disclosed: %s", instructions)
				}
				return
			}
			for _, want := range []string{"review", "Review code", "/skills/review/SKILL.md", "starlark_repl", "relative references"} {
				if !strings.Contains(instructions, want) {
					t.Errorf("instructions missing %q", want)
				}
			}
		})
	}
}

func TestCatalogExpand(t *testing.T) {
	catalog := Catalog{entries: []skill{
		{name: "review", path: "/skills/review/SKILL.md", invocable: byUser | byModel},
		{name: "publish", path: "/skills/publish/SKILL.md", invocable: byUser},
		{name: "background", invocable: byModel},
		{name: "disabled"},
	}}
	for _, tc := range []struct {
		name, input, path, wantError string
	}{
		{name: "ordinary prompt", input: "Review this code"},
		{name: "inline slash", input: "Explain /skill:review"},
		{name: "empty prompt"},
		{name: "both", input: "/skill:review", path: "/skills/review/SKILL.md"},
		{name: "user only with arguments", input: " /skill:publish version 2\nKeep $ARGUMENTS and !`commands` literal.", path: "/skills/publish/SKILL.md"},
		{name: "model only", input: "/skill:background", wantError: "not user-invocable"},
		{name: "disabled", input: "/skill:disabled", wantError: "not user-invocable"},
		{name: "unknown", input: "/skill:missing", wantError: "unknown skill"},
		{name: "traversal is not a name", input: "/skill:../review", wantError: "unknown skill"},
		{name: "missing name", input: "/skill:", wantError: "unknown skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := catalog.Expand(tc.input)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || got != "" {
					t.Fatalf("expanded = %q, err = %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.path == "" {
				if got != tc.input {
					t.Fatalf("ordinary prompt changed: %q", got)
				}
				return
			}
			if !strings.HasPrefix(got, tc.input+"\n\n") || !strings.Contains(got, tc.path) || !strings.Contains(got, "starlark_repl") || !strings.Contains(got, filepath.Dir(tc.path)) {
				t.Fatalf("expanded prompt lost input or activation instructions: %q", got)
			}
		})
	}
}

func writeSkill(t *testing.T, root, name, metadata string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n"+metadata+"---\nBody stays on disk."), 0600); err != nil {
		t.Fatal(err)
	}
}
