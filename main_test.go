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

	// Test case 1: Struct from separate package
	t.Run("StructFromSeparatePackage", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "pkg/types.go", `
package pkg
type MyStruct struct {}
`)
		file2 := createFile(tempDir, "main.go", `
package main
import "myproject/pkg"
func useMyStruct(s pkg.MyStruct) {}
`)
		result, err := resolveTypeFiles([]string{file1, file2})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true}, result)
	})

	// Test case 2: Interface defined in separate file
	t.Run("InterfaceInSeparateFile", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "interfaces.go", `
package main
type MyInterface interface {
	DoSomething()
}
`)
		file2 := createFile(tempDir, "implementation.go", `
package main
type MyStruct struct{}
func (m MyStruct) DoSomething() {}
`)
		file3 := createFile(tempDir, "usage.go", `
package main
func useInterface(i MyInterface) {}
`)
		result, err := resolveTypeFiles([]string{file1, file2, file3})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true, file3: true}, result)
	})

	// Test case 3: Multiple packages with cross-package type usage
	t.Run("CrossPackageTypeUsage", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "pkg1/types.go", `
package pkg1
type Type1 struct{}
`)
		file2 := createFile(tempDir, "pkg2/types.go", `
package pkg2
import "myproject/pkg1"
type Type2 struct {
	Field pkg1.Type1
}
`)
		file3 := createFile(tempDir, "main.go", `
package main
import (
	"myproject/pkg1"
	"myproject/pkg2"
)
func useTypes(t1 pkg1.Type1, t2 pkg2.Type2) {}
`)
		result, err := resolveTypeFiles([]string{file1, file2, file3})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true, file3: true}, result)
	})

	// Test case 4: Embedded interface from another package
	t.Run("EmbeddedInterfaceFromAnotherPackage", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "pkg/interfaces.go", `
package pkg
type BaseInterface interface {
	BaseMethod()
}
`)
		file2 := createFile(tempDir, "main.go", `
package main
import "myproject/pkg"
type ExtendedInterface interface {
	pkg.BaseInterface
	ExtendedMethod()
}
`)
		result, err := resolveTypeFiles([]string{file1, file2})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true}, result)
	})

	// Test case 5: Type alias and named import
	t.Run("TypeAliasAndNamedImport", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "pkg/types.go", `
package pkg
type OriginalType struct{}
`)
		file2 := createFile(tempDir, "main.go", `
package main
import pkgalias "myproject/pkg"
type AliasType = pkgalias.OriginalType
func useAliasType(a AliasType) {}
`)
		result, err := resolveTypeFiles([]string{file1, file2})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true}, result)
	})

	// Test case 6: Empty input (keeping this useful case)
	t.Run("EmptyInput", func(t *testing.T) {
		result, err := resolveTypeFiles([]string{})
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	// Test case 7: Non-existent file (keeping this useful case)
	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := resolveTypeFiles([]string{"non_existent.go"})
		assert.Error(t, err)
	})

	// Test case 8: Generic types with constraints from another package
	t.Run("GenericTypesWithConstraints", func(t *testing.T) {
		tempDir := t.TempDir()
		file1 := createFile(tempDir, "pkg/constraints.go", `
package pkg
type Number interface {
	int | float64
}
`)
		file2 := createFile(tempDir, "main.go", `
package main
import "myproject/pkg"
type GenericType[T pkg.Number] struct {
	Value T
}
func useGenericType[T pkg.Number](g GenericType[T]) {}
`)
		result, err := resolveTypeFiles([]string{file1, file2})
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{file1: true, file2: true}, result)
	})
}
