package tools

import (
	"os"
	"testing"
)

func TestFileTools(t *testing.T) {
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
	
	// Test CreateFileTool
	t.Run("CreateFile", func(t *testing.T) {
		params := CreateFileParams{
			Path:     "test.txt",
			FileText: "Hello, world!",
		}
		
		result, err := CreateFileTool(params)
		if err != nil {
			t.Fatalf("CreateFileTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("CreateFileTool failed: %v", result.Content)
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
		result, err = CreateFileTool(params)
		if err != nil {
			t.Fatalf("CreateFileTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("CreateFileTool should have failed when creating an existing file")
		}
	})
	
	// Test EditFileTool
	t.Run("EditFile", func(t *testing.T) {
		params := EditFileParams{
			Path:   "test.txt",
			OldStr: "Hello, world!",
			NewStr: "Hello, CPE!",
		}
		
		result, err := EditFileTool(params)
		if err != nil {
			t.Fatalf("EditFileTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("EditFileTool failed: %v", result.Content)
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
		badParams := EditFileParams{
			Path:   "test.txt",
			OldStr: "This string does not exist",
			NewStr: "New content",
		}
		
		result, err = EditFileTool(badParams)
		if err != nil {
			t.Fatalf("EditFileTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("EditFileTool should have failed when old string doesn't exist")
		}
	})
	
	// Test MoveFileTool
	t.Run("MoveFile", func(t *testing.T) {
		// Create a subdirectory
		err := os.Mkdir("subdir", 0755)
		if err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}
		
		params := MoveFileParams{
			SourcePath: "test.txt",
			TargetPath: "subdir/renamed.txt",
		}
		
		result, err := MoveFileTool(params)
		if err != nil {
			t.Fatalf("MoveFileTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("MoveFileTool failed: %v", result.Content)
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
		if string(content) != "Hello, CPE!" {
			t.Errorf("File content does not match after move: got %q, want %q", string(content), "Hello, CPE!")
		}
	})
	
	// Test DeleteFileTool
	t.Run("DeleteFile", func(t *testing.T) {
		params := DeleteFileParams{
			Path: "subdir/renamed.txt",
		}
		
		result, err := DeleteFileTool(params)
		if err != nil {
			t.Fatalf("DeleteFileTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("DeleteFileTool failed: %v", result.Content)
		}
		
		// Check file no longer exists
		if _, err := os.Stat("subdir/renamed.txt"); !os.IsNotExist(err) {
			t.Error("File still exists after delete")
		}
		
		// Test deleting non-existent file
		result, err = DeleteFileTool(params)
		if err != nil {
			t.Fatalf("DeleteFileTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("DeleteFileTool should have failed when file doesn't exist")
		}
	})
	
	// Test ViewFileTool
	t.Run("ViewFile", func(t *testing.T) {
		// Create a test file with known content
		testContent := "This is a test file for ViewFileTool.\nIt has multiple lines.\n"
		err := os.WriteFile("view_test.txt", []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		
		params := ViewFileParams{
			Path: "view_test.txt",
		}
		
		result, err := ViewFileTool(params)
		if err != nil {
			t.Fatalf("ViewFileTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("ViewFileTool failed: %v", result.Content)
		}
		
		// Check content matches
		content, ok := result.Content.(string)
		if !ok {
			t.Fatalf("ViewFileTool result content is not a string")
		}
		if content != testContent {
			t.Errorf("ViewFileTool content does not match: got %q, want %q", content, testContent)
		}
		
		// Test viewing non-existent file
		badParams := ViewFileParams{
			Path: "nonexistent.txt",
		}
		
		result, err = ViewFileTool(badParams)
		if err != nil {
			t.Fatalf("ViewFileTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("ViewFileTool should have failed when file doesn't exist")
		}
		
		// Test viewing a directory
		dirParams := ViewFileParams{
			Path: "subdir",
		}
		
		result, err = ViewFileTool(dirParams)
		if err != nil {
			t.Fatalf("ViewFileTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("ViewFileTool should have failed when path is a directory")
		}
	})
}

func TestFolderTools(t *testing.T) {
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
	
	// Test CreateFolderTool
	t.Run("CreateFolder", func(t *testing.T) {
		params := CreateFolderParams{
			Path: "new_folder/nested",
		}
		
		result, err := CreateFolderTool(params)
		if err != nil {
			t.Fatalf("CreateFolderTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("CreateFolderTool failed: %v", result.Content)
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
		result, err = CreateFolderTool(params)
		if err != nil {
			t.Fatalf("CreateFolderTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("CreateFolderTool should have failed when creating an existing folder")
		}
	})
	
	// Test MoveFolderTool
	t.Run("MoveFolder", func(t *testing.T) {
		params := MoveFolderParams{
			SourcePath: "new_folder",
			TargetPath: "renamed_folder",
		}
		
		result, err := MoveFolderTool(params)
		if err != nil {
			t.Fatalf("MoveFolderTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("MoveFolderTool failed: %v", result.Content)
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
	
	// Test DeleteFolderTool with non-recursive delete
	t.Run("DeleteFolderNonRecursive", func(t *testing.T) {
		// Try to delete non-empty folder without recursive flag
		params := DeleteFolderParams{
			Path:      "renamed_folder",
			Recursive: false,
		}
		
		result, err := DeleteFolderTool(params)
		if err != nil {
			t.Fatalf("DeleteFolderTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("DeleteFolderTool should have failed when deleting non-empty folder without recursive flag")
		}
		
		// Delete empty nested folder
		emptyParams := DeleteFolderParams{
			Path:      "renamed_folder/nested",
			Recursive: false,
		}
		
		result, err = DeleteFolderTool(emptyParams)
		if err != nil {
			t.Fatalf("DeleteFolderTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("DeleteFolderTool failed to delete empty folder: %v", result.Content)
		}
		
		// Check nested folder no longer exists
		if _, err := os.Stat("renamed_folder/nested"); !os.IsNotExist(err) {
			t.Error("Nested folder still exists after delete")
		}
	})
	
	// Test DeleteFolderTool with recursive delete
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
		params := DeleteFolderParams{
			Path:      "renamed_folder",
			Recursive: true,
		}
		
		result, err := DeleteFolderTool(params)
		if err != nil {
			t.Fatalf("DeleteFolderTool returned error: %v", err)
		}
		if result.IsError {
			t.Errorf("DeleteFolderTool failed: %v", result.Content)
		}
		
		// Check folder no longer exists
		if _, err := os.Stat("renamed_folder"); !os.IsNotExist(err) {
			t.Error("Folder still exists after recursive delete")
		}
		
		// Test deleting non-existent folder
		result, err = DeleteFolderTool(params)
		if err != nil {
			t.Fatalf("DeleteFolderTool returned error: %v", err)
		}
		if !result.IsError {
			t.Error("DeleteFolderTool should have failed when folder doesn't exist")
		}
	})
}