package mcp

import (
	"context"
	"os"
	"testing"
)

func TestFolderFunctions(t *testing.T) {
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

	// Test ExecuteCreateFolder
	t.Run("CreateFolder", func(t *testing.T) {
		_, err := ExecuteCreateFolder(ctx, CreateFolderInput{
			Path: "new_folder/nested",
		})
		if err != nil {
			t.Fatalf("ExecuteCreateFolder returned error: %v", err)
		}

		// Check folder exists
		info, err := os.Stat("new_folder/nested")
		if err != nil {
			t.Fatalf("Failed to stat created folder: %v", err)
		}
		if !info.IsDir() {
			t.Error("Created path is not a directory")
		}

		// Test creating a folder that already exists
		_, err = ExecuteCreateFolder(ctx, CreateFolderInput{
			Path: "new_folder/nested",
		})
		if err == nil {
			t.Error("ExecuteCreateFolder should have failed when creating an existing folder")
		}
	})

	// Test ExecuteMoveFolder
	t.Run("MoveFolder", func(t *testing.T) {
		_, err := ExecuteMoveFolder(ctx, MoveFolderInput{
			SourcePath: "new_folder",
			TargetPath: "renamed_folder",
		})
		if err != nil {
			t.Fatalf("ExecuteMoveFolder returned error: %v", err)
		}

		// Check old folder doesn't exist
		if _, err := os.Stat("new_folder"); !os.IsNotExist(err) {
			t.Error("Source folder still exists after move")
		}

		// Check new folder exists
		info, err := os.Stat("renamed_folder/nested")
		if err != nil {
			t.Fatalf("Failed to stat moved folder: %v", err)
		}
		if !info.IsDir() {
			t.Error("Moved path is not a directory")
		}
	})

	// Test ExecuteDeleteFolder with non-recursive delete
	t.Run("DeleteFolderNonRecursive", func(t *testing.T) {
		// Try to delete non-empty folder without recursive flag
		_, err := ExecuteDeleteFolder(ctx, DeleteFolderInput{
			Path:      "renamed_folder",
			Recursive: false,
		})
		if err == nil {
			t.Error("ExecuteDeleteFolder should have failed when deleting non-empty folder without recursive flag")
		}

		// Delete empty nested folder
		_, err = ExecuteDeleteFolder(ctx, DeleteFolderInput{
			Path:      "renamed_folder/nested",
			Recursive: false,
		})
		if err != nil {
			t.Fatalf("ExecuteDeleteFolder returned error: %v", err)
		}

		// Check nested folder no longer exists
		if _, err := os.Stat("renamed_folder/nested"); !os.IsNotExist(err) {
			t.Error("Nested folder still exists after delete")
		}
	})

	// Test ExecuteDeleteFolder with recursive delete
	t.Run("DeleteFolderRecursive", func(t *testing.T) {
		// Create some nested content
		err := os.Mkdir("renamed_folder/new_nested", 0755)
		if err != nil {
			t.Fatalf("Failed to create nested directory: %v", err)
		}
		err = os.WriteFile("renamed_folder/new_nested/test.txt", []byte("test"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Delete folder recursively
		_, err = ExecuteDeleteFolder(ctx, DeleteFolderInput{
			Path:      "renamed_folder",
			Recursive: true,
		})
		if err != nil {
			t.Fatalf("ExecuteDeleteFolder returned error: %v", err)
		}

		// Check folder no longer exists
		if _, err := os.Stat("renamed_folder"); !os.IsNotExist(err) {
			t.Error("Folder still exists after recursive delete")
		}

		// Test deleting non-existent folder
		_, err = ExecuteDeleteFolder(ctx, DeleteFolderInput{
			Path:      "renamed_folder",
			Recursive: true,
		})
		if err == nil {
			t.Error("ExecuteDeleteFolder should have failed when folder doesn't exist")
		}
	})
}
