package ignore

import (
	"github.com/gobwas/glob"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadIgnoreFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create a .cpeignore file
	ignoreContent := []byte("*.txt\n#comment\n/dir/*\n")
	err := os.WriteFile(filepath.Join(tempDir, ".cpeignore"), ignoreContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create .cpeignore file: %v", err)
	}

	ir := NewIgnoreRules()
	err = ir.LoadIgnoreFile(tempDir)
	if err != nil {
		t.Fatalf("LoadIgnoreFile failed: %v", err)
	}

	if len(ir.patterns) != 2 {
		t.Errorf("Expected 2 patterns, got %d", len(ir.patterns))
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

func TestFindIgnoreFile(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) (string, string)
		expectedSuffix string
	}{
		{
			name: "ExistingIgnoreFile",
			setupFunc: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				ignoreFilePath := filepath.Join(tempDir, ".cpeignore")
				err := os.WriteFile(ignoreFilePath, []byte{}, 0644)
				if err != nil {
					t.Fatalf("Failed to create .cpeignore file: %v", err)
				}
				return tempDir, ignoreFilePath
			},
			expectedSuffix: ".cpeignore",
		},
		{
			name: "SubdirectoryWithParentIgnoreFile",
			setupFunc: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				subDir := filepath.Join(tempDir, "subdir")
				err := os.Mkdir(subDir, 0755)
				if err != nil {
					t.Fatalf("Failed to create subdirectory: %v", err)
				}
				ignoreFilePath := filepath.Join(tempDir, ".cpeignore")
				err = os.WriteFile(ignoreFilePath, []byte{}, 0644)
				if err != nil {
					t.Fatalf("Failed to create .cpeignore file: %v", err)
				}
				return subDir, ignoreFilePath
			},
			expectedSuffix: ".cpeignore",
		},
		{
			name: "NoIgnoreFile",
			setupFunc: func(t *testing.T) (string, string) {
				tempDir := t.TempDir()
				return tempDir, ""
			},
			expectedSuffix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startDir, expectedPath := tt.setupFunc(t)
			result := findIgnoreFile(startDir)

			if tt.expectedSuffix == "" {
				if result != "" {
					t.Errorf("findIgnoreFile(%q) = %q, want empty string", startDir, result)
				}
			} else {
				if !strings.HasSuffix(result, tt.expectedSuffix) {
					t.Errorf("findIgnoreFile(%q) = %q, want path ending with %q", startDir, result, tt.expectedSuffix)
				}
				if result != expectedPath {
					t.Errorf("findIgnoreFile(%q) = %q, want %q", startDir, result, expectedPath)
				}
			}
		})
	}
}
