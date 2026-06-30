package prompt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkills(t *testing.T) {
	t.Parallel()

	t.Run("returns name description path", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		createSkill(t, baseDir, "alpha-skill", "alpha-skill", "Alpha description")
		createSkill(t, baseDir, "beta-skill", "beta-skill", "Beta description")

		got := skills(&bytes.Buffer{}, baseDir)
		want := []Skill{
			{Name: "alpha-skill", Description: "Alpha description", Path: filepath.Join(baseDir, "alpha-skill")},
			{Name: "beta-skill", Description: "Beta description", Path: filepath.Join(baseDir, "beta-skill")},
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("skills() mismatch\nwant: %#v\n got: %#v", want, got)
		}
	})

	t.Run("skips invalid skills and emits warnings", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		createSkill(t, baseDir, "valid-skill", "valid-skill", "Valid description")
		createInvalidSkill(t, baseDir, "invalid-skill", "name: invalid-skill")

		var warnings bytes.Buffer
		got := skills(&warnings, baseDir)
		want := []Skill{{Name: "valid-skill", Description: "Valid description", Path: filepath.Join(baseDir, "valid-skill")}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("skills() mismatch\nwant: %#v\n got: %#v", want, got)
		}

		warnText := warnings.String()
		if !strings.Contains(warnText, `warning: failed to load skill "invalid-skill"`) {
			t.Fatalf("expected warning for invalid skill, got: %q", warnText)
		}
	})

	t.Run("follows symlinked skill directories", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		targetBaseDir := t.TempDir()
		createSkill(t, targetBaseDir, "actual-skill", "linked-skill", "Linked description")

		linkedSkillDir := filepath.Join(baseDir, "linked-skill")
		createSymlink(t, filepath.Join(targetBaseDir, "actual-skill"), linkedSkillDir)

		got := skills(&bytes.Buffer{}, baseDir)
		want := []Skill{{Name: "linked-skill", Description: "Linked description", Path: linkedSkillDir}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("skills() mismatch\nwant: %#v\n got: %#v", want, got)
		}
	})

	t.Run("reports broken symlink siblings", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		createSkill(t, baseDir, "valid-skill", "valid-skill", "Valid description")
		createSymlink(t, filepath.Join(baseDir, "missing-skill"), filepath.Join(baseDir, "broken-skill"))

		var warnings bytes.Buffer
		got := skills(&warnings, baseDir)
		want := []Skill{{Name: "valid-skill", Description: "Valid description", Path: filepath.Join(baseDir, "valid-skill")}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("skills() mismatch\nwant: %#v\n got: %#v", want, got)
		}

		warnText := warnings.String()
		if !strings.Contains(warnText, `warning: failed to inspect skill "broken-skill"`) {
			t.Fatalf("expected warning for broken symlink, got: %q", warnText)
		}
	})

	t.Run("reports symlink loops", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		createSkill(t, baseDir, "valid-skill", "valid-skill", "Valid description")
		createSymlink(t, "loop-skill", filepath.Join(baseDir, "loop-skill"))

		var warnings bytes.Buffer
		got := skills(&warnings, baseDir)
		want := []Skill{{Name: "valid-skill", Description: "Valid description", Path: filepath.Join(baseDir, "valid-skill")}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("skills() mismatch\nwant: %#v\n got: %#v", want, got)
		}

		warnText := warnings.String()
		if !strings.Contains(warnText, `warning: failed to inspect skill "loop-skill"`) {
			t.Fatalf("expected warning for symlink loop, got: %q", warnText)
		}
	})

	t.Run("reports symlink files", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		createSkill(t, baseDir, "valid-skill", "valid-skill", "Valid description")
		filePath := filepath.Join(baseDir, "not-a-skill.md")
		if err := os.WriteFile(filePath, []byte("not a skill"), 0o644); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
		createSymlink(t, filePath, filepath.Join(baseDir, "file-symlink"))

		var warnings bytes.Buffer
		got := skills(&warnings, baseDir)
		want := []Skill{{Name: "valid-skill", Description: "Valid description", Path: filepath.Join(baseDir, "valid-skill")}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("skills() mismatch\nwant: %#v\n got: %#v", want, got)
		}

		warnText := warnings.String()
		if !strings.Contains(warnText, `warning: failed to inspect skill "file-symlink"`) {
			t.Fatalf("expected warning for symlink file, got: %q", warnText)
		}
	})

	t.Run("skips missing paths", func(t *testing.T) {
		t.Parallel()

		got := skills(&bytes.Buffer{}, "/path/that/does/not/exist")
		if len(got) != 0 {
			t.Fatalf("expected no skills, got: %#v", got)
		}
	})

	t.Run("returns empty when no paths provided", func(t *testing.T) {
		t.Parallel()

		got := skills(&bytes.Buffer{})
		if len(got) != 0 {
			t.Fatalf("expected no skills, got: %#v", got)
		}
	})
}

func TestSystemPromptTemplateSupportsCustomSkillFormatting(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	createSkill(t, baseDir, "alpha-skill", "alpha-skill", "Alpha description")

	tmpl := fmt.Sprintf(`{{- range $s := skills %q -}}{{$s.Name}}={{$s.Description}}@{{$s.Path}};{{- end -}}`, baseDir)

	out, err := SystemPromptTemplate(context.Background(), tmpl, TemplateData{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("SystemPromptTemplate() error = %v", err)
	}

	want := fmt.Sprintf("alpha-skill=Alpha description@%s;", filepath.Join(baseDir, "alpha-skill"))
	if out != want {
		t.Fatalf("SystemPromptTemplate() mismatch\nwant: %q\n got: %q", want, out)
	}
}

func createSkill(t *testing.T, baseDir, folder, name, description string) {
	t.Helper()

	skillDir := filepath.Join(baseDir, folder)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	content := fmt.Sprintf(`---
name: %s
description: %s
---

# %s
`, name, description, name)
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
