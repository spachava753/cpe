package agent

import (
	"os"
	"path/filepath"
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

func TestTemplateFunctionIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "data.txt")
	if err := os.WriteFile(testFile, []byte("test data"), 0644); err != nil {
		t.Fatal(err)
	}

	sysInfo := &SystemInfo{
		CurrentDate: "2024-01-01",
		WorkingDir:  tmpDir,
	}

	templateStr := `Date: {{.CurrentDate}}
File exists: {{fileExists "` + testFile + `"}}
File content: {{includeFile "` + testFile + `"}}
Command output: {{exec "echo processed"}}
Upper sprig: {{ upper "hello" }}`

	result, err := sysInfo.ExecuteTemplateString(templateStr)
	if err != nil {
		t.Fatal(err)
	}

	expected := `Date: 2024-01-01
File exists: true
File content: test data
Command output: processed
Upper sprig: HELLO`

	if result != expected {
		t.Errorf("Template execution mismatch\nGot:\n%s\nWant:\n%s", result, expected)
	}
}
