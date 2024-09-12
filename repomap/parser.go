package repomap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
)

// ParseRepo walks the repository and parses Go files
func ParseRepo(fsys fs.FS) (*RepoMap, error) {
	repo := &RepoMap{}

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			fileMap, err := parseFile(fsys, path)
			if err != nil {
				return fmt.Errorf("error parsing file %s: %w", path, err)
			}
			repo.Files = append(repo.Files, fileMap)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %w", err)
	}

	// Sort files by path for consistent output
	sort.Slice(repo.Files, func(i, j int) bool {
		return repo.Files[i].Path < repo.Files[j].Path
	})

	return repo, nil
}

// parseFile parses a single Go file and extracts relevant information
func parseFile(fsys fs.FS, path string) (*FileMap, error) {
	fset := token.NewFileSet()
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	node, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	fileMap := &FileMap{
		Path:        path,
		PackageName: node.Name.Name,
		Methods:     make(map[string][]*ast.FuncDecl),
	}

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.IMPORT:
				for _, spec := range d.Specs {
					if importSpec, ok := spec.(*ast.ImportSpec); ok {
						fileMap.Imports = append(fileMap.Imports, importSpec)
					}
				}
			case token.TYPE:
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						switch typeSpec.Type.(type) {
						case *ast.StructType:
							fileMap.Structs = append(fileMap.Structs, typeSpec)
						case *ast.InterfaceType:
							fileMap.Interfaces = append(fileMap.Interfaces, typeSpec)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvType := d.Recv.List[0].Type
				var typeName string
				switch t := recvType.(type) {
				case *ast.StarExpr:
					typeName = t.X.(*ast.Ident).Name
				case *ast.Ident:
					typeName = t.Name
				}
				fileMap.Methods[typeName] = append(fileMap.Methods[typeName], d)
			} else {
				fileMap.Functions = append(fileMap.Functions, d)
			}
		}
	}

	return fileMap, nil
}
