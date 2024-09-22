package codemap

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// GenerateOutputFromAST creates the XML-like output for the code map using AST
func GenerateOutputFromAST(fsys fs.FS) (string, error) {
	var sb strings.Builder
	sb.WriteString("<code_map>\n")

	var filePaths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".go" {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error walking directory: %w", err)
	}

	sort.Strings(filePaths)

	for _, path := range filePaths {
		fileContent, err := generateFileOutput(fsys, path)
		if err != nil {
			return "", fmt.Errorf("error generating output for file %s: %w", path, err)
		}
		sb.WriteString(fileContent)
	}

	sb.WriteString("</code_map>\n")
	return sb.String(), nil
}

func generateFileOutput(fsys fs.FS, path string) (string, error) {
	fset := token.NewFileSet()
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return "", err
	}

	// Walk the AST and remove function bodies and their associated comments
	astutil.Apply(file, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.FuncDecl:
			n.Body = nil
			// Remove comments associated with the function body
			n.Doc = nil
		case *ast.BlockStmt:
			// Remove comments inside block statements (including function bodies)
			n.List = nil
		}
		return true
	}, nil)

	// Remove all comments that are not associated with declarations
	file.Comments = nil

	// Format the modified AST
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return "", fmt.Errorf("error formatting AST: %w", err)
	}

	return fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n%s</file_map>\n</file>\n", path, buf.String()), nil
}
