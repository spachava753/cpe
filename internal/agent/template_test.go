package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileExists(t *testing.T) {
	// Create temp file for testing
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"existing file", testFile, true},
		{"non-existent file", filepath.Join(tmpDir, "missing.txt"), false},
		{"directory", tmpDir, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fileExists(tt.path)
			if result != tt.expected {
				t.Errorf("fileExists(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIncludeFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	testContent := "Hello, World!"
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	emptyFile := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"existing file", testFile, testContent},
		{"empty file", emptyFile, ""},
		{"non-existent file", filepath.Join(tmpDir, "missing.txt"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := includeFile(tt.path)
			if result != tt.expected {
				t.Errorf("includeFile(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestExecCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"simple echo", "echo hello", "hello"},
		{"echo with spaces", "echo 'hello world'", "hello world"},
		{"command with pipe", "echo test | wc -c", "5"},
		{"failing command", "false", ""},
		{"non-existent command", "nonexistentcommand123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := execCommand(tt.command)
			if result != tt.expected {
				t.Errorf("execCommand(%q) = %q, want %q", tt.command, result, tt.expected)
			}
		})
	}
}

func TestSkills(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid skill directory
	validSkillDir := filepath.Join(tmpDir, "pdf-processing")
	if err := os.MkdirAll(validSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	validSkillMd := `---
name: pdf-processing
description: Extract text and tables from PDF files.
license: MIT
---

# PDF Processing Skill

This skill helps with PDF operations.
`
	if err := os.WriteFile(filepath.Join(validSkillDir, "SKILL.md"), []byte(validSkillMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another valid skill
	codeReviewDir := filepath.Join(tmpDir, "code-review")
	if err := os.MkdirAll(codeReviewDir, 0755); err != nil {
		t.Fatal(err)
	}
	codeReviewMd := `---
name: code-review
description: Performs code reviews with focus on security.
---
# Code Review Skill
`
	if err := os.WriteFile(filepath.Join(codeReviewDir, "SKILL.md"), []byte(codeReviewMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an invalid skill (missing name)
	invalidSkillDir := filepath.Join(tmpDir, "invalid-skill")
	if err := os.MkdirAll(invalidSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	invalidSkillMd := `---
description: Missing name field
---
# Invalid
`
	if err := os.WriteFile(filepath.Join(invalidSkillDir, "SKILL.md"), []byte(invalidSkillMd), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a directory without SKILL.md
	noSkillDir := filepath.Join(tmpDir, "not-a-skill")
	if err := os.MkdirAll(noSkillDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Run("single valid skills directory", func(t *testing.T) {
		result := skills(tmpDir)
		if !strings.Contains(result, "<skills>") {
			t.Error("expected <skills> tag in output")
		}
		if !strings.Contains(result, `<skill name="pdf-processing">`) {
			t.Error("expected pdf-processing skill in output")
		}
		if !strings.Contains(result, `<skill name="code-review">`) {
			t.Error("expected code-review skill in output")
		}
		if strings.Contains(result, "invalid-skill") {
			t.Error("invalid skill should not be in output")
		}
		if strings.Contains(result, "not-a-skill") {
			t.Error("directory without SKILL.md should not be in output")
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		result := skills("/nonexistent/path")
		if result != "" {
			t.Errorf("expected empty string for non-existent path, got %q", result)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		emptyDir := filepath.Join(tmpDir, "empty")
		if err := os.MkdirAll(emptyDir, 0755); err != nil {
			t.Fatal(err)
		}
		result := skills(emptyDir)
		if result != "" {
			t.Errorf("expected empty string for empty directory, got %q", result)
		}
	})

	t.Run("multiple directories", func(t *testing.T) {
		anotherDir := filepath.Join(tmpDir, "another")
		if err := os.MkdirAll(filepath.Join(anotherDir, "my-skill"), 0755); err != nil {
			t.Fatal(err)
		}
		mySkillMd := `---
name: my-skill
description: Another skill.
---
# My Skill
`
		if err := os.WriteFile(filepath.Join(anotherDir, "my-skill", "SKILL.md"), []byte(mySkillMd), 0644); err != nil {
			t.Fatal(err)
		}

		result := skills(tmpDir, anotherDir)
		if !strings.Contains(result, `<skill name="pdf-processing">`) {
			t.Error("expected pdf-processing skill in output")
		}
		if !strings.Contains(result, `<skill name="my-skill">`) {
			t.Error("expected my-skill skill in output")
		}
	})
}

func TestIsValidSkillName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"simple lowercase", "myskill", true},
		{"with hyphen", "my-skill", true},
		{"with numbers", "skill123", true},
		{"complex valid", "my-skill-v2", true},
		{"uppercase", "MySkill", false},
		{"underscore", "my_skill", false},
		{"starts with hyphen", "-skill", false},
		{"ends with hyphen", "skill-", false},
		{"double hyphen", "my--skill", false},
		{"empty", "", false},
		{"too long", strings.Repeat("a", 65), false},
		{"max length", strings.Repeat("a", 64), true},
		{"special chars", "skill@name", false},
		{"spaces", "my skill", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidSkillName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidSkillName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expected    string
		expectError bool
	}{
		{
			name: "valid frontmatter",
			content: `---
name: test
description: A test skill
---
# Content
`,
			expected: "name: test\ndescription: A test skill",
		},
		{
			name:        "no frontmatter",
			content:     "# Just markdown",
			expectError: true,
		},
		{
			name:        "empty content",
			content:     "",
			expectError: true,
		},
		{
			name: "frontmatter with extra fields",
			content: `---
name: skill
description: desc
license: MIT
metadata:
  author: test
---
Body
`,
			expected: "name: skill\ndescription: desc\nlicense: MIT\nmetadata:\n  author: test",
		},
		{
			name:     "frontmatter with CRLF line endings",
			content:  "---\r\nname: skill\r\ndescription: desc\r\n---\r\nBody",
			expected: "name: skill\r\ndescription: desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractFrontmatter(tt.content)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("extractFrontmatter() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseSkill(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid skill", func(t *testing.T) {
		skillDir := filepath.Join(tmpDir, "valid-skill")
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		skillMd := `---
name: valid-skill
description: A valid skill for testing.
---
# Valid Skill
`
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(skillMd), 0644); err != nil {
			t.Fatal(err)
		}

		skill, err := parseSkill(skillPath, skillDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if skill.Name != "valid-skill" {
			t.Errorf("Name = %q, want %q", skill.Name, "valid-skill")
		}
		if skill.Description != "A valid skill for testing." {
			t.Errorf("Description = %q, want %q", skill.Description, "A valid skill for testing.")
		}
		if skill.Path != skillDir {
			t.Errorf("Path = %q, want %q", skill.Path, skillDir)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		skillDir := filepath.Join(tmpDir, "no-name")
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		skillMd := `---
description: Missing name field.
---
# No Name
`
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(skillMd), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := parseSkill(skillPath, skillDir)
		if err == nil {
			t.Error("expected error for missing name")
		}
	})

	t.Run("invalid name format", func(t *testing.T) {
		skillDir := filepath.Join(tmpDir, "bad-name")
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		skillMd := `---
name: Invalid_Name
description: Has invalid name.
---
# Bad Name
`
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(skillMd), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := parseSkill(skillPath, skillDir)
		if err == nil {
			t.Error("expected error for invalid name format")
		}
	})
}
