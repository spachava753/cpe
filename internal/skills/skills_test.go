package skills

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscover(t *testing.T) {
	t.Run("discovers project and global skills", func(t *testing.T) {
		cwd := t.TempDir()
		home := t.TempDir()
		createSkill(t, filepath.Join(cwd, ".agents", "skills"), "project-skill", map[string]any{
			"name":        "project-skill",
			"description": "Project description",
			"group":       "project",
		})
		createSkill(t, filepath.Join(home, ".agents", "skills"), "global-skill", map[string]any{
			"name":        "global-skill",
			"description": "Global description",
		})

		got := Discover(t.Context(), DiscoverOptions{Cwd: cwd, HomeDir: home})

		want := []Skill{
			{
				Name:        "project-skill",
				Description: "Project description",
				Path:        "./.agents/skills/project-skill",
				AbsPath:     filepath.Join(cwd, ".agents", "skills", "project-skill"),
				Metadata: map[string]any{
					"name":        "project-skill",
					"description": "Project description",
					"group":       "project",
				},
			},
			{
				Name:        "global-skill",
				Description: "Global description",
				Path:        "~/.agents/skills/global-skill",
				AbsPath:     filepath.Join(home, ".agents", "skills", "global-skill"),
				Metadata: map[string]any{
					"name":        "global-skill",
					"description": "Global description",
				},
			},
		}
		if !reflect.DeepEqual(got.Skills, want) {
			t.Fatalf("Discover() mismatch\nwant: %#v\n got: %#v", want, got.Skills)
		}
	})

	t.Run("project duplicate wins over global", func(t *testing.T) {
		cwd := t.TempDir()
		home := t.TempDir()
		createSkill(t, filepath.Join(cwd, ".agents", "skills"), "same-skill", map[string]any{
			"name":        "same-skill",
			"description": "Project description",
		})
		createSkill(t, filepath.Join(home, ".agents", "skills"), "same-skill", map[string]any{
			"name":        "same-skill",
			"description": "Global description",
		})

		warnings := captureSkillLogs(t)
		got := Discover(t.Context(), DiscoverOptions{Cwd: cwd, HomeDir: home})

		if len(got.Skills) != 1 {
			t.Fatalf("Discover() returned %d skills, want 1: %#v", len(got.Skills), got.Skills)
		}
		if got.Skills[0].Description != "Project description" {
			t.Fatalf("duplicate winner description = %q, want project", got.Skills[0].Description)
		}
		if !strings.Contains(warnings.String(), `msg="ignoring duplicate skill"`) || !strings.Contains(warnings.String(), `skill=same-skill`) {
			t.Fatalf("expected duplicate warning, got %q", warnings.String())
		}
	})

	t.Run("skips invalid skills and emits warnings", func(t *testing.T) {
		cwd := t.TempDir()
		baseDir := filepath.Join(cwd, ".agents", "skills")
		createSkill(t, baseDir, "valid-skill", map[string]any{
			"name":        "valid-skill",
			"description": "Valid description",
		})
		createInvalidSkill(t, baseDir, "invalid-skill", "name: invalid-skill")

		warnings := captureSkillLogs(t)
		got := Discover(t.Context(), DiscoverOptions{Cwd: cwd, HomeDir: t.TempDir()})

		if len(got.Skills) != 1 || got.Skills[0].Name != "valid-skill" {
			t.Fatalf("Discover() = %#v, want valid-skill only", got.Skills)
		}
		if !strings.Contains(warnings.String(), `msg="failed to load skill"`) || !strings.Contains(warnings.String(), `skill=invalid-skill`) {
			t.Fatalf("expected invalid warning, got %q", warnings.String())
		}
	})

	t.Run("follows symlinked skill directories", func(t *testing.T) {
		cwd := t.TempDir()
		baseDir := filepath.Join(cwd, ".agents", "skills")
		targetBaseDir := t.TempDir()
		createSkill(t, targetBaseDir, "actual-skill", map[string]any{
			"name":        "linked-skill",
			"description": "Linked description",
		})
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			t.Fatalf("os.MkdirAll() error = %v", err)
		}
		createSymlink(t, filepath.Join(targetBaseDir, "actual-skill"), filepath.Join(baseDir, "linked-skill"))

		got := Discover(t.Context(), DiscoverOptions{Cwd: cwd, HomeDir: t.TempDir()})

		if len(got.Skills) != 1 || got.Skills[0].Name != "linked-skill" || got.Skills[0].Path != "./.agents/skills/linked-skill" {
			t.Fatalf("Discover() = %#v, want linked skill", got.Skills)
		}
	})

	t.Run("reports broken symlink siblings", func(t *testing.T) {
		cwd := t.TempDir()
		baseDir := filepath.Join(cwd, ".agents", "skills")
		createSkill(t, baseDir, "valid-skill", map[string]any{
			"name":        "valid-skill",
			"description": "Valid description",
		})
		createSymlink(t, filepath.Join(baseDir, "missing-skill"), filepath.Join(baseDir, "broken-skill"))

		warnings := captureSkillLogs(t)
		got := Discover(t.Context(), DiscoverOptions{Cwd: cwd, HomeDir: t.TempDir()})

		if len(got.Skills) != 1 || got.Skills[0].Name != "valid-skill" {
			t.Fatalf("Discover() = %#v, want valid-skill only", got.Skills)
		}
		if !strings.Contains(warnings.String(), `msg="failed to inspect skill"`) || !strings.Contains(warnings.String(), `skill=broken-skill`) {
			t.Fatalf("expected broken symlink warning, got %q", warnings.String())
		}
	})
}

func TestCatalogModelVisible(t *testing.T) {
	catalog := Catalog{Skills: []Skill{
		{Name: "visible", Description: "Visible"},
		{Name: "hidden", Description: "Hidden", DisableModelInvocation: true},
	}}

	got := catalog.ModelVisible()
	want := []Skill{{Name: "visible", Description: "Visible"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ModelVisible() mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func captureSkillLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &logs
}

func createSkill(t *testing.T, baseDir, folder string, metadata map[string]any) {
	t.Helper()

	skillDir := filepath.Join(baseDir, folder)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	var frontmatter strings.Builder
	for _, key := range []string{"name", "description", "disable-model-invocation", "group"} {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		fmt.Fprintf(&frontmatter, "%s: %v\n", key, value)
	}
	content := fmt.Sprintf("---\n%s---\n\n# %s\n", frontmatter.String(), metadata["name"])
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func createInvalidSkill(t *testing.T, baseDir, folder, frontmatter string) {
	t.Helper()

	skillDir := filepath.Join(baseDir, folder)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	content := fmt.Sprintf("---\n%s\n---\n\n# Invalid\n", frontmatter)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func createSymlink(t *testing.T, oldname, newname string) {
	t.Helper()

	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("os.Symlink() error = %v", err)
	}
}
