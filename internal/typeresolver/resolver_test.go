package typeresolver

import (
	"fmt"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/stretchr/testify/assert"
	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// TestPrintTreeSitterTree is a utility function that prints the tree-sitter AST for a given file
func TestPrintTreeSitterTree(t *testing.T) {
	path := "../../main.go"
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", path, err)
	}

	parser := sitter.NewParser()
	defer parser.Close()
	goLang := sitter.NewLanguage(golang.Language())
	err = parser.SetLanguage(goLang)
	if err != nil {
		t.Fatalf("error setting language: %s", err)
	}

	tree := parser.Parse(content, nil)
	defer tree.Close()

	var printNode func(node *sitter.Node, level int)
	printNode = func(node *sitter.Node, level int) {
		indent := strings.Repeat("  ", level)
		nodeText := node.Kind()
		if node.ChildCount() == 0 {
			nodeText += fmt.Sprintf(": '%s'", node.Utf8Text(content))
		}
		t.Logf("%s%s\n", indent, nodeText)

		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			printNode(child, level+1)
		}
	}

	t.Logf("\nTree-sitter AST for %s:\n", path)
	t.Logf(strings.Repeat("=", 40))
	printNode(tree.RootNode(), 0)
	t.Logf(strings.Repeat("=", 40))
}

func TestExtractGoSymbols(t *testing.T) {
	parser := sitter.NewParser()
	defer parser.Close()

	tests := []struct {
		name            string
		content         string
		expectedQueries []string
		wantErr         bool
	}{
		{
			name: "Simple type and function usage",
			content: `
package main

func ProcessData(data CustomType) error {
	result := helper.ProcessCustomType(data)
	return nil
}`,
			expectedQueries: []string{
				`
		(type_declaration
			[
				(type_spec
					name: (type_identifier) @type.definition)
				(type_alias
					name: (type_identifier) @type.definition)
			]
			(#any-of? @type.definition "CustomType")
		)
	`,
				`
		(
			[
				(function_declaration
					name: (identifier) @func.definition)
				(method_declaration
					name: (field_identifier) @func.definition)
			]
			(#any-of? @func.definition "ProcessCustomType")
		)
`,
			},
			wantErr: false,
		},
		{
			name: "Complex type usage with methods",
			content: `
package main

type Handler struct {
	processor DataProcessor
	cache     Cache
}

func (h *Handler) HandleRequest(req Request) Response {
	data := h.processor.Process(req.Data())
	return NewResponse(data)
}`,
			expectedQueries: []string{
				`
		(type_declaration
			[
				(type_spec
					name: (type_identifier) @type.definition)
				(type_alias
					name: (type_identifier) @type.definition)
			]
			(#any-of? @type.definition "Cache" "DataProcessor" "Request" "Response")
		)
	`,
				`
		(
			[
				(function_declaration
					name: (identifier) @func.definition)
				(method_declaration
					name: (field_identifier) @func.definition)
			]
			(#any-of? @func.definition "Data" "NewResponse" "Process")
		)
	`,
			},
			wantErr: false,
		},
		{
			name: "Generic types and builtin types",
			content: `
package main

type Result[T any] struct {
	data    T
	err     error
	status  string
	counter int
}

func ProcessGeneric[T CustomConstraint](input Result[T]) {
	processor := NewProcessor[T]()
	processor.Process(input.data)
}`,
			expectedQueries: []string{
				`
		(type_declaration
			[
				(type_spec
					name: (type_identifier) @type.definition)
				(type_alias
					name: (type_identifier) @type.definition)
			]
			(#any-of? @type.definition "CustomConstraint")
		)
	`,
				`
		(
			[
				(function_declaration
					name: (identifier) @func.definition)
				(method_declaration
					name: (field_identifier) @func.definition)
			]
			(#any-of? @func.definition "NewProcessor" "Process")
		)
	`,
			},
			wantErr: false,
		},
		{
			name: "Interface definitions and implementations",
			content: `
package main

type Service interface {
	Process(data CustomData) (Result, error)
	Validate(input ValidationInput) bool
}

type serviceImpl struct {
	validator Validator
	processor Processor
}

func (s *serviceImpl) Process(data CustomData) (Result, error) {
	return s.processor.ProcessData(data)
}`,
			expectedQueries: []string{
				`
		(type_declaration
			[
				(type_spec
					name: (type_identifier) @type.definition)
				(type_alias
					name: (type_identifier) @type.definition)
			]
			(#any-of? @type.definition "CustomData" "Processor" "Result" "ValidationInput" "Validator")
		)
	`,
				`
		(
			[
				(function_declaration
					name: (identifier) @func.definition)
				(method_declaration
					name: (field_identifier) @func.definition)
			]
			(#any-of? @func.definition "ProcessData")
		)
	`,
			},
			wantErr: false,
		},
		{
			name: "Empty content",
			content: `
package main
`,
			expectedQueries: []string{},
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries, err := extractGoSymbols([]byte(tt.content), parser)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, len(tt.expectedQueries), len(queries), "number of queries should match")

			// Compare queries after normalizing whitespace
			for i := range queries {
				expectedNormalized := strings.Join(strings.Fields(tt.expectedQueries[i]), " ")
				actualNormalized := strings.Join(strings.Fields(queries[i]), " ")
				assert.Equal(t, expectedNormalized, actualNormalized,
					"Query %d doesn't match\nExpected:\n%s\n\nGot:\n%s",
					i, tt.expectedQueries[i], queries[i])
			}
		})
	}
}

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
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"usage.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg/types.go": true, "main.go": true}, result)
	})

	// Test case 6: Empty input
	t.Run("EmptyInput", func(t *testing.T) {
		fsys := createTestFS(map[string]string{})
		result, err := ResolveTypeAndFunctionFiles([]string{}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	// Test case 7: Non-existent file
	t.Run("NonExistentFile", func(t *testing.T) {
		fsys := createTestFS(map[string]string{})
		_, err := ResolveTypeAndFunctionFiles([]string{"non_existent.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"usage.go", "other.go"}, fsys, ignore.NewIgnoreRules())
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
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"main.go":       true,
			"pkg1/types.go": true,
			"pkg2/types.go": true,
		}, result)
	})

	// Test case 11: Using imported type from standard library
	t.Run("UsingImportedTypeFromStdLib", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"main.go": `
package main
import (
	"net/http"
	"fmt"
)
func handleRequest(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"main.go": true}, result)
	})
	// Test case 12: Function defined in separate file
	t.Run("FunctionDefinedInSeparateFile", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"utils.go": `
package main
func HelperFunction(s string) string {
	return s + " processed"
}
`,
			"main.go": `
package main
func main() {
	result := HelperFunction("test")
	println(result)
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"utils.go": true, "main.go": true}, result)
	})

	// Test case 13: Function from separate package
	t.Run("FunctionFromSeparatePackage", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"pkg/utils.go": `
package pkg
func UtilFunction(i int) int {
	return i * 2
}
`,
			"main.go": `
package main
import "myproject/pkg"
func main() {
	result := pkg.UtilFunction(5)
	println(result)
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{"pkg/utils.go": true, "main.go": true}, result)
	})

	// Test case 14: Multiple function definitions and usages
	t.Run("MultipleFunctionDefinitionsAndUsages", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"math/operations.go": `
package math
func Add(a, b int) int {
	return a + b
}
func Multiply(a, b int) int {
	return a * b
}
`,
			"utils/formatter.go": `
package utils
import "fmt"
func FormatResult(operation string, result int) string {
	return fmt.Sprintf("Result of %s: %d", operation, result)
}
`,
			"main.go": `
package main
import (
	"myproject/math"
	"myproject/utils"
)
func main() {
	sum := math.Add(5, 3)
	product := math.Multiply(4, 2)
	println(utils.FormatResult("addition", sum))
	println(utils.FormatResult("multiplication", product))
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"math/operations.go": true,
			"utils/formatter.go": true,
			"main.go":            true,
		}, result)
	})

	// Test case 15: Function with custom type parameters
	t.Run("FunctionWithCustomTypeParameters", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"types/custom.go": `
package types
type CustomType struct {
	Value string
}
`,
			"operations/process.go": `
package operations
import "myproject/types"
func ProcessCustomType(ct types.CustomType) string {
	return "Processed: " + ct.Value
}
`,
			"main.go": `
package main
import (
	"myproject/types"
	"myproject/operations"
)
func main() {
	ct := types.CustomType{Value: "test"}
	result := operations.ProcessCustomType(ct)
	println(result)
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"types/custom.go":       true,
			"operations/process.go": true,
			"main.go":               true,
		}, result)
	})

	// Test case 16: Function resolution with unnecessary files
	t.Run("FunctionResolutionWithUnnecessaryFiles", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"math/operations.go": `
package math
func Add(a, b int) int {
	return a + b
}
`,
			"utils/helper.go": `
package utils
func HelperFunction() string {
	return "I'm a helper"
}
`,
			"main.go": `
package main
import "myproject/math"
func main() {
	result := math.Add(5, 3)
	println(result)
}
`,
			"unused.go": `
package main
func UnusedFunction() {
	// This function is not used
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"math/operations.go": true,
			"main.go":            true,
		}, result)
		assert.NotContains(t, result, "utils/helper.go")
		assert.NotContains(t, result, "unused.go")
	})

	// Test case 17: Type resolution with unnecessary files
	t.Run("TypeResolutionWithUnnecessaryFiles", func(t *testing.T) {
		fsys := createTestFS(map[string]string{
			"models/user.go": `
package models
type User struct {
	ID   int
	Name string
}
`,
			"models/product.go": `
package models
type Product struct {
	ID    int
	Title string
}
`,
			"main.go": `
package main
import "myproject/models"
func main() {
	user := models.User{ID: 1, Name: "John"}
	println(user.Name)
}
`,
			"unused_types.go": `
package main
type UnusedType struct {
	Field string
}
`,
		})
		result, err := ResolveTypeAndFunctionFiles([]string{"main.go"}, fsys, ignore.NewIgnoreRules())
		assert.NoError(t, err)
		assert.Equal(t, map[string]bool{
			"models/user.go": true,
			"main.go":        true,
		}, result)
		assert.NotContains(t, result, "models/product.go")
		assert.NotContains(t, result, "unused_types.go")
	})
}
