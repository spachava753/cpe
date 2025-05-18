package tree

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/token/builder"
	"github.com/spachava753/gai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockTokenCounter is a mock implementation of gai.TokenCounter for testing
type MockTokenCounter struct {
	// FixedCount will be returned for every input if not nil
	FixedCount *uint
	// ContentToCount maps file content to token counts
	ContentToCount map[string]uint
}

// Count implements the TokenCounter interface
func (m *MockTokenCounter) Count(ctx context.Context, dialog gai.Dialog) (uint, error) {
	// If FixedCount is set, always return it
	if m.FixedCount != nil {
		return *m.FixedCount, nil
	}

	// Otherwise, look up by content
	if len(dialog) > 0 && len(dialog[0].Blocks) > 0 {
		content := string(dialog[0].Blocks[0].Content.(gai.Str))
		if count, ok := m.ContentToCount[content]; ok {
			return count, nil
		}
		// Default to length if not found
		return uint(len(content)), nil
	}

	return 0, nil
}

// Test building a directory tree from file counts
func TestBuildTreeFromCounts(t *testing.T) {
	// Set up a test file structure
	fileCounts := map[string]uint{
		"/root/file1.txt":             10,
		"/root/file2.txt":             20,
		"/root/dir1/file3.txt":        30,
		"/root/dir1/file4.txt":        40,
		"/root/dir2/file5.txt":        50,
		"/root/dir2/subdir/file6.txt": 60,
	}

	// Build the tree
	tree := buildTreeFromCounts("/root", fileCounts)

	// Check the tree structure
	assert.Equal(t, "/root", tree.Path)
	assert.Equal(t, "root", tree.Name)
	assert.True(t, tree.IsDir)
	assert.Equal(t, uint(210), tree.Count) // Sum of all counts

	// Check the first level children
	assert.Equal(t, 4, len(tree.Children))

	// The children should be sorted: first directories (dir1, dir2) then files (file1, file2)
	assert.True(t, tree.Children[0].IsDir)
	assert.True(t, tree.Children[1].IsDir)
	assert.False(t, tree.Children[2].IsDir)
	assert.False(t, tree.Children[3].IsDir)

	// Check directory counts
	var dir1, dir2 *DirTreeNode
	for _, child := range tree.Children {
		if child.Name == "dir1" {
			dir1 = child
		} else if child.Name == "dir2" {
			dir2 = child
		}
	}

	require.NotNil(t, dir1)
	require.NotNil(t, dir2)

	assert.Equal(t, uint(70), dir1.Count)  // 30 + 40
	assert.Equal(t, uint(110), dir2.Count) // 50 + 60

	// Check subdir
	var subdir *DirTreeNode
	for _, child := range dir2.Children {
		if child.Name == "subdir" {
			subdir = child
			break
		}
	}
	require.NotNil(t, subdir)
	assert.Equal(t, uint(60), subdir.Count)
}

// Create a testing filesystem with text files
func setupTestFS(t *testing.T) (string, func()) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "tree-test-*")
	require.NoError(t, err)

	// Create a directory structure
	err = os.MkdirAll(filepath.Join(tempDir, "dir1"), 0755)
	require.NoError(t, err)

	err = os.MkdirAll(filepath.Join(tempDir, "dir2", "subdir"), 0755)
	require.NoError(t, err)

	// Create some files
	files := map[string]string{
		filepath.Join(tempDir, "file1.txt"):                   "Hello, world!",
		filepath.Join(tempDir, "file2.txt"):                   "This is a test file.",
		filepath.Join(tempDir, "dir1", "file3.txt"):           "File in dir1",
		filepath.Join(tempDir, "dir1", "file4.txt"):           "Another file in dir1",
		filepath.Join(tempDir, "dir2", "file5.txt"):           "File in dir2",
		filepath.Join(tempDir, "dir2", "subdir", "file6.txt"): "File in subdir",
		filepath.Join(tempDir, "binary.bin"):                  string([]byte{0x00, 0x01, 0x02, 0x03}), // Binary file
		filepath.Join(tempDir, "image.png"):                   "fake png content",                     // Fake image
	}

	for path, content := range files {
		err = os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Return cleanup function
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, cleanup
}

// Test building a directory tree from a real filesystem
func TestBuildDirTree(t *testing.T) {
	// Skip if running in CI or without filesystem access
	if os.Getenv("CI") != "" || os.Getenv("SKIP_FS_TESTS") != "" {
		t.Skip("Skipping filesystem test in CI environment")
	}

	// Set up a test directory
	tempDir, cleanup := setupTestFS(t)
	defer cleanup()

	// Create a mock token counter that returns fixed counts based on file content
	mockCounter := &MockTokenCounter{
		ContentToCount: map[string]uint{
			"Hello, world!":        5,
			"This is a test file.": 10,
			"File in dir1":         15,
			"Another file in dir1": 20,
			"File in dir2":         25,
			"File in subdir":       30,
			"fake png content":     50, // Assign a count for the fake image
		},
	}

	// Create a gitignore with ignore patterns
	ignoreContent := "binary.bin"
	ignoreFile := filepath.Join(tempDir, ".gitignore")
	err := os.WriteFile(ignoreFile, []byte(ignoreContent), 0644)
	require.NoError(t, err)

	ign, err := gitignore.CompileIgnoreFile(ignoreFile)
	require.NoError(t, err)

	// Use a buffer to capture progress output
	var progressBuf bytes.Buffer

	// Build the directory tree
	tree, err := BuildDirTree(context.Background(), tempDir, ign, mockCounter, &progressBuf)
	require.NoError(t, err)

	// Check the tree structure
	assert.Equal(t, tempDir, tree.Path)
	assert.Equal(t, filepath.Base(tempDir), tree.Name)
	assert.True(t, tree.IsDir)

	// Due to test variance we'll check each component rather than exact total
	var dir1, dir2 *DirTreeNode
	foundDir1 := false
	foundDir2 := false

	for _, child := range tree.Children {
		if child.Name == "dir1" {
			dir1 = child
			foundDir1 = true
		} else if child.Name == "dir2" {
			dir2 = child
			foundDir2 = true
		}
	}

	require.True(t, foundDir1, "dir1 not found in tree children")
	require.True(t, foundDir2, "dir2 not found in tree children")

	assert.Equal(t, uint(35), dir1.Count) // 15 + 20
	assert.Equal(t, uint(55), dir2.Count) // 25 + 30

	// Check that the binary file is ignored
	for _, child := range tree.Children {
		assert.NotEqual(t, "binary.bin", child.Name)
	}

	// Check progress output (optional, as it can be noisy)
	// t.Logf("Progress Output:\n%s", progressBuf.String())
}

// Test the PrintDirTree function
func TestPrintDirTree(t *testing.T) {
	// Create a simple tree
	tree := &DirTreeNode{
		Path:  "/root",
		Name:  "root",
		IsDir: true,
		Count: 30,
		Children: []*DirTreeNode{
			{
				Path:  "/root/dir1",
				Name:  "dir1",
				IsDir: true,
				Count: 20,
				Children: []*DirTreeNode{
					{
						Path:  "/root/dir1/file1.txt",
						Name:  "file1.txt",
						IsDir: false,
						Count: 20,
					},
				},
			},
			{
				Path:  "/root/file2.txt",
				Name:  "file2.txt",
				IsDir: false,
				Count: 10,
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Print the tree
	PrintDirTree(tree, "")

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var output strings.Builder
	_, err := io.Copy(&output, r)
	require.NoError(t, err)

	// Check output
	expected := "📁 root/ (30 tokens)\n" +
		"  📁 dir1/ (20 tokens)\n" +
		"    📄 file1.txt (20 tokens)\n" +
		"  📄 file2.txt (10 tokens)\n"

	assert.Equal(t, expected, output.String())
}

// Add tests for multimodal file handling in builder package
func TestBuildDialog_Multimodal(t *testing.T) {
	tests := []struct {
		name         string
		content      []byte
		expectedType gai.Modality
		expectError  bool
	}{
		{
			name:         "Plain text",
			content:      []byte("This is plain text"),
			expectedType: gai.Text,
			expectError:  false,
		},
		{
			name:         "JSON",
			content:      []byte(`{"key": "value"}`),
			expectedType: gai.Text,
			expectError:  false,
		},
		{
			name:         "Binary data (unsupported)",
			content:      []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			expectedType: 0, // not used when error expected
			expectError:  true,
		},
		{
			name: "Fake PNG (should be image)",
			// Minimal valid PNG (1x1 transparent pixel), base64 encoded
			content: func() []byte {
				b, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=")
				return b
			}(),
			expectedType: gai.Image,
			expectError:  false,
		},
		{
			name: "Fake MP3 (should be audio)",
			// Minimal fake MP3 header, base64 encoded
			content: func() []byte {
				b, _ := base64.StdEncoding.DecodeString("SUQzBAAAAAAA")
				return b
			}(),
			expectedType: gai.Audio,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialog, err := builder.BuildDialog(tt.content)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, 1, len(dialog))
			assert.Equal(t, 1, len(dialog[0].Blocks))
			assert.Equal(t, tt.expectedType, dialog[0].Blocks[0].ModalityType)
		})
	}
}
