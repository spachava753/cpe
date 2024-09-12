package repomap

import (
	"go/ast"
)

// RepoMap represents the entire repository
type RepoMap struct {
	Files []*FileMap
}

// FileMap represents a single Go file
type FileMap struct {
	Path           string
	PackageName    string
	PackageComment string
	Imports        []*ast.ImportSpec
	Structs        []*ast.TypeSpec
	Interfaces     []*ast.TypeSpec
	Functions      []*ast.FuncDecl
	Methods        map[string][]*ast.FuncDecl
	Comments       map[ast.Node]string
	FieldComments  map[*ast.Field]string
	StructComments map[*ast.TypeSpec]string
}
