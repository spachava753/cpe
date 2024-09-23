package main

import (
	"github.com/stretchr/testify/assert"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestResolveTypeFiles(t *testing.T) {
	// Helper function to create an in-memory file system
	createTestFS := func(files map[string]string) fs.FS {
		fsys := fstest.MapFS{}
		for path, content := range files {
			fsys[path] = &fstest.MapFile{Data: []byte(content)}
		}
		return fsys
	}

	// Test case 1: Struct from separate package
	t.Run("StructFromSeparatePackage", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg/types.go": `
package pkg
type MyStruct struct {}
`,
			"main.go": `
package main
import "myproject/pkg"
func useMyStruct(s pkg.MyStruct) {}
`,
		})
		result, err := resolveTypeFiles([]string{"main.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg/types.go": true, "main.go": true}, result)
	})

	// Test case 2: Interface defined in separate file
	t.Run("InterfaceInSeparateFile", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"interfaces.go": `
package main
type MyInterface interface {
	DoSomething()
}
`,
			"implementation.go": `
package main
type MyStruct struct{}
func (m MyStruct) DoSomething() {}
`,
			"usage.go": `
package main
func useInterface(i MyInterface) {}
`,
		})
		result, err := resolveTypeFiles([]string{"usage.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"interfaces.go": true, "usage.go": true}, result)
	})

	// Test case 3: Multiple packages with cross-package type usage
	t.Run("CrossPackageTypeUsage", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg1/types.go": `
package pkg1
type Type1 struct{}
`,
			"pkg2/types.go": `
package pkg2
import "myproject/pkg1"
type Type2 struct {
	Field pkg1.Type1
}
`,
			"main.go": `
package main
import (
	"myproject/pkg1"
	"myproject/pkg2"
)
func useTypes(t1 pkg1.Type1, t2 pkg2.Type2) {}
`,
		})
		result, err := resolveTypeFiles([]string{"main.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg1/types.go": true, "pkg2/types.go": true, "main.go": true}, result)
	})

	// Test case 4: Embedded interface from another package
	t.Run("EmbeddedInterfaceFromAnotherPackage", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg/interfaces.go": `
package pkg
type BaseInterface interface {
	BaseMethod()
}
`,
			"main.go": `
package main
import "myproject/pkg"
type ExtendedInterface interface {
	pkg.BaseInterface
	ExtendedMethod()
}
`,
		})
		result, err := resolveTypeFiles([]string{"main.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg/interfaces.go": true, "main.go": true}, result)
	})

	// Test case 5: Type alias and named import
	t.Run("TypeAliasAndNamedImport", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg/types.go": `
package pkg
type OriginalType struct{}
`,
			"main.go": `
package main
import pkgalias "myproject/pkg"
type AliasType = pkgalias.OriginalType
func useAliasType(a AliasType) {}
`,
		})
		result, err := resolveTypeFiles([]string{"main.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg/types.go": true, "main.go": true}, result)
	})

	// Test case 6: Empty input
	t.Run("EmptyInput", func(t *testing.T) {
		fsys := createTestFS(map[string]string{})
		result, err := resolveTypeFiles([]string{}, fsys)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	// Test case 7: Non-existent file
	t.Run("NonExistentFile", func(t *testing.T) {
		fsys := createTestFS(map[string]string{})
		_, err := resolveTypeFiles([]string{"non_existent.go"}, fsys)
		assert.Error(t, err)
	})

	// Test case 8: Generic types with constraints from another package
	t.Run("GenericTypesWithConstraints", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg/constraints.go": `
package pkg
type Number interface {
	int | float64
}
`,
			"main.go": `
package main
import "myproject/pkg"
type GenericType[T pkg.Number] struct {
	Value T
}
func useGenericType[T pkg.Number](g GenericType[T]) {}
`,
		})
		result, err := resolveTypeFiles([]string{"main.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg/constraints.go": true, "main.go": true}, result)
	})

	// Test case 9: Type usage in a file not in selectedFiles
	t.Run("TypeUsageInNonSelectedFile", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"types.go": `
package main
type MyType struct{}
`,
			"usage.go": `
package main
func useMyType(m MyType) {}
`,
			"other.go": `
package main
func doSomething() {}
`,
		})
		result, err := resolveTypeFiles([]string{"usage.go", "other.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"types.go": true, "usage.go": true, "other.go": true}, result)
	})

	// Test case 10: Types with the same name in different packages
	t.Run("SameNameTypesInDifferentPackages", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg1/types.go": `
package pkg1
type User struct {
	ID   int
	Name string
}
`,
			"pkg2/types.go": `
package pkg2
type User struct {
	Email    string
	Password string
}
`,
			"main.go": `
package main
import (
	"myproject/pkg1"
	"myproject/pkg2"
)
func processUsers(u1 pkg1.User, u2 pkg2.User) {
	// Do something with both User types
}
`,
		})
		result, err := resolveTypeFiles([]string{"main.go"}, fsys)
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"main.go":       true,
			"pkg1/types.go": true,
			"pkg2/types.go": true,
		}, result)
	})
}
