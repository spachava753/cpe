package agent

import (
	"context"
	"os"
	"testing"
)

func TestFileFunctions(t *testing.T) {
	// Setup test directory using t.TempDir() for automatic cleanup
	testDir := t.TempDir()

	// Change to test directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	err = os.Chdir(testDir)
	if err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}
	defer os.Chdir(origDir)

	ctx := context.Background()

	// Test ExecuteCreateFile
	t.Run("CreateFile", func(t *testing.T) {
		_, err := ExecuteCreateFile(ctx, CreateFileInput{
			Path:     "test.txt",
			FileText: "Hello, world!",
		})
		if err != nil {
			t.Fatalf("ExecuteCreateFile returned error: %v", err)
		}

		// Check file exists and has correct content
		content, err := os.ReadFile("test.txt")
		if err != nil {
			t.Fatalf("Failed to read created file: %v", err)
		}
		if string(content) != "Hello, world!" {
			t.Errorf("File content does not match: got %q, want %q", string(content), "Hello, world!")
		}

		// Test creating a file that already exists
		_, err = ExecuteCreateFile(ctx, CreateFileInput{
			Path:     "test.txt",
			FileText: "Hello, world!",
		})
		if err == nil {
			t.Error("ExecuteCreateFile should have failed when creating an existing file")
		}
	})

	// Test ExecuteEditFile
	t.Run("EditFile", func(t *testing.T) {
		_, err := ExecuteEditFile(ctx, EditFileInput{
			Path:   "test.txt",
			OldStr: "Hello, world!",
			NewStr: "Hello, CPE!",
		})
		if err != nil {
			t.Fatalf("ExecuteEditFile returned error: %v", err)
		}

		// Check file has updated content
		content, err := os.ReadFile("test.txt")
		if err != nil {
			t.Fatalf("Failed to read edited file: %v", err)
		}
		if string(content) != "Hello, CPE!" {
			t.Errorf("File content does not match after edit: got %q, want %q", string(content), "Hello, CPE!")
		}

		// Test editing with non-existent old string
		_, err = ExecuteEditFile(ctx, EditFileInput{
			Path:   "test.txt",
			OldStr: "This string does not exist",
			NewStr: "New content",
		})
		if err == nil {
			t.Error("ExecuteEditFile should have failed when old string doesn't exist")
		}

		// Test deletion
		_, err = ExecuteEditFile(ctx, EditFileInput{
			Path:   "test.txt",
			OldStr: "Hello, CPE!",
			NewStr: "",
		})
		if err != nil {
			t.Fatalf("ExecuteEditFile (delete) returned error: %v", err)
		}

		content, err = os.ReadFile("test.txt")
		if err != nil {
			t.Fatalf("Failed to read file after delete: %v", err)
		}
		if string(content) != "" {
			t.Errorf("Delete should leave file empty, got: %q", string(content))
		}

		// Test append (put some content back first)
		_, err = ExecuteEditFile(ctx, EditFileInput{
			Path:   "test.txt",
			OldStr: "",
			NewStr: "Appended content\n",
		})
		if err != nil {
			t.Fatalf("ExecuteEditFile (append) returned error: %v", err)
		}

		content, err = os.ReadFile("test.txt")
		if err != nil {
			t.Fatalf("Failed to read file after append: %v", err)
		}
		if string(content) != "Appended content\n" {
			t.Errorf("Append failed, got: %q", string(content))
		}

		// Test error if both old_str and new_str are empty
		_, err = ExecuteEditFile(ctx, EditFileInput{Path: "test.txt"})
		if err == nil {
			t.Error("ExecuteEditFile should fail if both old_str and new_str are empty")
		}
	})

	// Test ExecuteMoveFile
	t.Run("MoveFile", func(t *testing.T) {
		// Create a subdirectory
		err := os.Mkdir("subdir", 0755)
		if err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}

		_, err = ExecuteMoveFile(ctx, MoveFileInput{
			SourcePath: "test.txt",
			TargetPath: "subdir/renamed.txt",
		})
		if err != nil {
			t.Fatalf("ExecuteMoveFile returned error: %v", err)
		}

		// Check old file doesn't exist
		if _, err := os.Stat("test.txt"); !os.IsNotExist(err) {
			t.Error("Source file still exists after move")
		}

		// Check new file exists with correct content
		content, err := os.ReadFile("subdir/renamed.txt")
		if err != nil {
			t.Fatalf("Failed to read moved file: %v", err)
		}
		if string(content) != "Appended content\n" {
			t.Errorf("File content does not match after move: got %q", string(content))
		}
	})

	// Test ExecuteDeleteFile
	t.Run("DeleteFile", func(t *testing.T) {
		_, err := ExecuteDeleteFile(ctx, DeleteFileInput{
			Path: "subdir/renamed.txt",
		})
		if err != nil {
			t.Fatalf("ExecuteDeleteFile returned error: %v", err)
		}

		// Check file no longer exists
		if _, err := os.Stat("subdir/renamed.txt"); !os.IsNotExist(err) {
			t.Error("File still exists after delete")
		}

		// Test deleting non-existent file
		_, err = ExecuteDeleteFile(ctx, DeleteFileInput{
			Path: "subdir/renamed.txt",
		})
		if err == nil {
			t.Error("ExecuteDeleteFile should have failed when file doesn't exist")
		}
	})

	// Test ExecuteViewFile
	t.Run("ViewFile", func(t *testing.T) {
		// Create a test file with known content
		testContent := "This is a test file for ViewFileTool.\nIt has multiple lines.\n"
		err := os.WriteFile("view_test.txt", []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		content, err := ExecuteViewFile(ctx, ViewFileInput{
			Path: "view_test.txt",
		})
		if err != nil {
			t.Fatalf("ExecuteViewFile returned error: %v", err)
		}

		// Check content matches
		if content != testContent {
			t.Errorf("ViewFileTool content does not match: got %q, want %q", content, testContent)
		}

		// Test viewing non-existent file
		_, err = ExecuteViewFile(ctx, ViewFileInput{
			Path: "nonexistent.txt",
		})
		if err == nil {
			t.Error("ExecuteViewFile should have failed when file doesn't exist")
		}

		// Test viewing a directory
		_, err = ExecuteViewFile(ctx, ViewFileInput{
			Path: "subdir",
		})
		if err == nil {
			t.Error("ExecuteViewFile should have failed when path is a directory")
		}
	})
}
