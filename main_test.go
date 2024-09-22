package main

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTypeFiles(t *testing.T) {
	// Helper function to create test files
	createFile := func(dir, name, content string) string {
		path := filepath.Join(dir, name)
		err := os.WriteFile(path, []byte(content), 0644)
		assert.NoError(t, err)
		return path
	}

	// Test case 1: Single file with type definition
	t.Run("SingleFileWithTypeDefinition", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "file1.go", `
package test
type MyType struct {}
`)
		result, err := resolveTypeFiles([]string{file1})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true}, result)
	})

	// Test case 2: Multiple files with type definition and usage
	t.Run("MultipleFilesWithTypeDefinitionAndUsage", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "file1.go", `
package test
type MyType struct {}
`)
		file2 := createFile(tempDir, "file2.go", `
package test
import "fmt"
func useMyType(m MyType) {
	fmt.Println(m)
}
`)
		result, err := resolveTypeFiles([]string{file1, file2})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true}, result)
	})

	// Test case 3: File with type usage but not in selected files
	t.Run("FileWithTypeUsageNotInSelectedFiles", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "file1.go", `
package test
type MyType struct {}
`)
		_ = createFile(tempDir, "file2.go", `
package test
import "fmt"
func useMyType(m MyType) {
	fmt.Println(m)
}
`)
		result, err := resolveTypeFiles([]string{file1})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true}, result)
	})

	// Test case 4: Multiple type definitions and usages across files
	t.Run("MultipleTypeDefinitionsAndUsages", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "file1.go", `
package test
type Type1 struct {}
type Type2 struct {}
`)
		file2 := createFile(tempDir, "file2.go", `
package test
type Type3 struct {
	T1 Type1
	T2 Type2
}
`)
		file3 := createFile(tempDir, "file3.go", `
package test
func useTypes(t1 Type1, t2 Type2, t3 Type3) {}
`)
		result, err := resolveTypeFiles([]string{file1, file2, file3})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true, file3: true}, result)
	})

	// Test case 5: Empty input
	t.Run("EmptyInput", func(t *testing.T) {
		result, err := resolveTypeFiles([]string{})
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	// Test case 6: Non-existent file
	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := resolveTypeFiles([]string{"non_existent.go"})
		assert.Error(t, err)
	})
}
