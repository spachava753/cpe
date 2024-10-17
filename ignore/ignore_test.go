package ignore

import (
	"github.com/gobwas/glob"
	"os"
	"path/filepath"
	"testing"
)

func TestNewIgnoreRules(t *testing.T) {
	ir := NewIgnoreRules()
	if ir == nil {
		t.Fatal("NewIgnoreRules returned nil")
	}
	if len(ir.patterns) != 0 {
		t.Errorf("Expected empty patterns, got %d patterns", len(ir.patterns))
	}
}

func TestLoadIgnoreFiles(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "path", "to", "repo")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdirectories: %v", err)
	}

	// Create multiple .cpeignore files
	ignoreFiles := map[string]string{
		filepath.Join(tempDir, ".cpeignore"): `*.txt`,
		filepath.Join(tempDir, "path", ".cpeignore"): `#comment
/dir/*`,
		filepath.Join(tempDir, "path", "to", ".cpeignore"): `*.md`,
		filepath.Join(subDir, ".cpeignore"): `*.go
#another comment`,
	}

	for path, content := range ignoreFiles {
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create .cpeignore file at %s: %v", path, err)
		}
	}

	ir := NewIgnoreRules()
	err = ir.LoadIgnoreFiles(subDir)
	if err != nil {
		t.Fatalf("LoadIgnoreFiles failed: %v", err)
	}

	expectedPatterns := 4 // *.txt, /dir/*, *.md, *.go
	if len(ir.patterns) != expectedPatterns {
		t.Errorf("Expected %d patterns, got %d", expectedPatterns, len(ir.patterns))
	}

	// Test if patterns from all ignore files are loaded
	testCases := []struct {
		path     string
		expected bool
	}{
		{"file.txt", true},
		{"/dir/file.go", true},
		{"README.md", true},
		{"main.go", true},
		{"file.jpg", false},
	}

	for _, tc := range testCases {
		if ir.ShouldIgnore(tc.path) != tc.expected {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", tc.path, !tc.expected, tc.expected)
		}
	}
}

func TestShouldIgnore(t *testing.T) {
	ir := NewIgnoreRules()
	ir.patterns = []glob.Glob{
		glob.MustCompile("*.txt"),
		glob.MustCompile("/dir/*"),
	}

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.txt", true},
		{"file.go", false},
		{"/dir/file.go", true},
		{"/other/file.txt", true},
		{"/other/file.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := ir.ShouldIgnore(tt.path)
			if result != tt.expected {
				t.Errorf("ShouldIgnore(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestFindIgnoreFiles(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) (string, []string)
		expectedCount int
	}{
		{
			name: "MultipleIgnoreFiles",
			setupFunc: func(t *testing.T) (string, []string) {
				tempDir := t.TempDir()
				subDir := filepath.Join(tempDir, "path", "to", "repo")
				err := os.MkdirAll(subDir, 0755)
				if err != nil {
					t.Fatalf("Failed to create subdirectories: %v", err)
				}

				ignoreFiles := []string{
					filepath.Join(tempDir, ".cpeignore"),
					filepath.Join(tempDir, "path", ".cpeignore"),
					filepath.Join(tempDir, "path", "to", ".cpeignore"),
					filepath.Join(subDir, ".cpeignore"),
				}

				for _, path := range ignoreFiles {
					err := os.WriteFile(path, []byte{}, 0644)
					if err != nil {
						t.Fatalf("Failed to create .cpeignore file at %s: %v", path, err)
					}
				}

				return subDir, ignoreFiles
			},
			expectedCount: 4,
		},
		{
			name: "NoIgnoreFiles",
			setupFunc: func(t *testing.T) (string, []string) {
				tempDir := t.TempDir()
				return tempDir, []string{}
			},
			expectedCount: 0,
		},
		{
			name: "SingleIgnoreFile",
			setupFunc: func(t *testing.T) (string, []string) {
				tempDir := t.TempDir()
				ignoreFile := filepath.Join(tempDir, ".cpeignore")
				err := os.WriteFile(ignoreFile, []byte{}, 0644)
				if err != nil {
					t.Fatalf("Failed to create .cpeignore file: %v", err)
				}
				return tempDir, []string{ignoreFile}
			},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startDir, expectedFiles := tt.setupFunc(t)
			result := findIgnoreFiles(startDir)

			if len(result) != tt.expectedCount {
				t.Errorf("findIgnoreFiles(%q) returned %d files, want %d", startDir, len(result), tt.expectedCount)
			}

			for _, expectedFile := range expectedFiles {
				found := false
				for _, resultFile := range result {
					if resultFile == expectedFile {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("findIgnoreFiles(%q) did not return expected file: %s", startDir, expectedFile)
				}
			}
		})
	}
}
