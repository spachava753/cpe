package agent

import (
	"context"
	"os"
	"path/filepath" // Added
	"strings"       // Added
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

func TestExecuteEditFile_SyntaxChecking(t *testing.T) {
	ctx := context.Background()

	// Valid Go code
	validGoContent := `package main

import "fmt"

// A comment
func main() {
	fmt.Println("Hello, world!")
}
`
	// Go code that will become invalid if "func" is removed
	goContentMakeInvalidByDeletion := `package main
func main() {}
`
	// Go code that will become invalid if "}" is removed
	goContentMakeInvalidByMissingBrace := `package main
func main() {
	// comment
}
`

	// Go code with an existing syntax error
	invalidGoContentOriginal := `package main

func main() {
	fmt.Println("Hello" // Missing closing parenthesis
}
`

	tests := []struct {
		name              string
		filename          string
		initialContent    string
		editInput         EditFileInput
		wantErr           bool
		wantErrMsgContain string // Substring to check in error message
		wantContent       string // Expected content if successful or if edit bypasses syntax check
		expectNoChange    bool   // True if file content should remain initialContent on error
	}{
		// --- Go Files (.go) - Parser Available ---
		{
			name:           "Go: Valid original, valid edit (replace comment)",
			filename:       "test.go",
			initialContent: validGoContent,
			editInput: EditFileInput{
				Path:   "test.go", // Path will be updated in the loop
				OldStr: "// A comment",
				NewStr: "// An edited comment",
			},
			wantErr:     false,
			wantContent: strings.Replace(validGoContent, "// A comment", "// An edited comment", 1),
		},
		{
			name:           "Go: Valid original, edit introduces syntax error (incomplete var decl)",
			filename:       "test.go",
			initialContent: validGoContent,
			editInput: EditFileInput{
				Path:   "test.go",
				OldStr: `fmt.Println("Hello, world!")`,
				NewStr: `var a =`,
			},
			wantErr:           true,
			wantErrMsgContain: "edit introduces syntax errors",
			expectNoChange:    true,
		},
		{
			name:           "Go: Original has syntax error, edit applied (add newline)",
			filename:       "test_invalid.go",
			initialContent: invalidGoContentOriginal,
			editInput: EditFileInput{
				Path:   "test_invalid.go",
				OldStr: "", // Append
				NewStr: "\n// appended comment",
			},
			wantErr:     false, // Syntax check bypassed due to original error
			wantContent: invalidGoContentOriginal + "\n// appended comment",
		},
		{
			name:           "Go: Valid original, valid append",
			filename:       "test_append.go",
			initialContent: validGoContent,
			editInput: EditFileInput{
				Path:   "test_append.go",
				OldStr: "", // Append
				NewStr: "\n// appended line",
			},
			wantErr:     false,
			wantContent: validGoContent + "\n// appended line",
		},
		{
			name:           "Go: Valid original, append introduces syntax error",
			filename:       "test_append_invalid.go",
			initialContent: validGoContent,
			editInput: EditFileInput{
				Path:   "test_append_invalid.go",
				OldStr: "", // Append
				NewStr: "\nfunc oops {",
			},
			wantErr:           true,
			wantErrMsgContain: "edit introduces syntax errors",
			expectNoChange:    true,
		},
		{
			name:           "Go: Valid original, valid delete",
			filename:       "test_delete.go",
			initialContent: validGoContent,
			editInput: EditFileInput{
				Path:   "test_delete.go",
				OldStr: "// A comment\n", // Delete the comment and its newline
				NewStr: "",
			},
			wantErr:     false,
			wantContent: strings.Replace(validGoContent, "// A comment\n", "", 1),
		},
		{
			name:           "Go: Valid original, delete introduces syntax error (delete 'func')",
			filename:       "test_delete_invalid.go",
			initialContent: goContentMakeInvalidByDeletion,
			editInput: EditFileInput{
				Path:   "test_delete_invalid.go",
				OldStr: "func ",
				NewStr: "",
			},
			wantErr:           true,
			wantErrMsgContain: "edit introduces syntax errors",
			expectNoChange:    true,
		},
		{
			name:           "Go: Valid original, delete introduces syntax error (delete '}')",
			filename:       "test_delete_brace_invalid.go",
			initialContent: goContentMakeInvalidByMissingBrace,
			editInput: EditFileInput{
				Path:   "test_delete_brace_invalid.go",
				OldStr: "}",
				NewStr: "",
			},
			wantErr:           true,
			wantErrMsgContain: "edit introduces syntax errors",
			expectNoChange:    true,
		},

		// --- Text Files (.txt) - No Parser Available ---
		{
			name:           "Text: Valid edit, no syntax check",
			filename:       "test.txt",
			initialContent: "Hello text world.",
			editInput: EditFileInput{
				Path:   "test.txt",
				OldStr: "text world",
				NewStr: "CPE user",
			},
			wantErr:     false,
			wantContent: "Hello CPE user.",
		},
		{
			name:           "Text: Append, no syntax check",
			filename:       "test_append.txt",
			initialContent: "Initial line.",
			editInput: EditFileInput{
				Path:   "test_append.txt",
				OldStr: "", // Append
				NewStr: "\nAppended line.",
			},
			wantErr:     false,
			wantContent: "Initial line.\nAppended line.",
		},

		// --- Standard Edit Errors ---
		{
			name:           "Go: old_str not found (valid Go file)",
			filename:       "test_std_err.go",
			initialContent: validGoContent,
			editInput: EditFileInput{
				Path:   "test_std_err.go",
				OldStr: "nonExistentString",
				NewStr: "replacement",
			},
			wantErr:           true,
			wantErrMsgContain: "old_str not found", // Standard error from EditFile
			expectNoChange:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, tc.filename)

			// Create initial file
			err := os.WriteFile(filePath, []byte(tc.initialContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write initial file %s: %v", filePath, err)
			}

			// Update path in editInput
			editInput := tc.editInput
			editInput.Path = filePath

			_, err = ExecuteEditFile(ctx, editInput)

			if tc.wantErr {
				if err == nil {
					t.Errorf("ExecuteEditFile expected an error, but got nil")
				} else if tc.wantErrMsgContain != "" && !strings.Contains(err.Error(), tc.wantErrMsgContain) {
					t.Errorf("ExecuteEditFile error message %q does not contain %q", err.Error(), tc.wantErrMsgContain)
				}
			} else {
				if err != nil {
					t.Errorf("ExecuteEditFile expected no error, but got: %v", err)
				}
			}

			// Verify file content
			finalContentBytes, readErr := os.ReadFile(filePath)
			if readErr != nil {
				t.Fatalf("Failed to read file %s after edit attempt: %v", filePath, readErr)
			}
			finalContent := string(finalContentBytes)

			expectedFinalContent := tc.initialContent
			if !tc.wantErr || (tc.wantErr && !tc.expectNoChange) { // if no error, or error but we expect change (e.g. original syntax error)
				expectedFinalContent = tc.wantContent
			}

			if finalContent != expectedFinalContent {
				t.Errorf("File content mismatch.\nExpected:\n%s\nGot:\n%s", expectedFinalContent, finalContent)
			}
		})
	}
}
