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
		Path:          path,
		PackageName:   node.Name.Name,
		Methods:       make(map[string][]*ast.FuncDecl),
		Comments:      make(map[ast.Node]string),
		FieldComments: make(map[*ast.Field]string),
		TypeComments:  make(map[*ast.TypeSpec]string),
	}

	if node.Doc != nil {
		fileMap.PackageComment = node.Doc.Text()
	}

	// Collect comments for all nodes
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if doc, ok := n.(ast.Node); ok && doc != nil {
			switch t := n.(type) {
			case *ast.GenDecl:
				if t.Doc != nil {
					fileMap.Comments[t] = t.Doc.Text()
				}
			case *ast.Field:
				if t.Doc != nil {
					fileMap.FieldComments[t] = t.Doc.Text()
				}
			case *ast.FuncDecl:
				if t.Doc != nil {
					fileMap.Comments[t] = t.Doc.Text()
				}
			case *ast.TypeSpec:
				if t.Doc != nil {
					fileMap.TypeComments[t] = t.Doc.Text()
				}
				if iface, ok := t.Type.(*ast.InterfaceType); ok {
					for _, method := range iface.Methods.List {
						if method.Doc != nil {
							fileMap.FieldComments[method] = method.Doc.Text()
						}
					}
				}
			}
		}
		return true
	})

	for _, decl := range node.Decls {
		fileMap.Declarations = append(fileMap.Declarations, decl)
		switch d := decl.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.IMPORT:
				for _, spec := range d.Specs {
					if importSpec, ok := spec.(*ast.ImportSpec); ok {
						fileMap.Imports = append(fileMap.Imports, importSpec)
					}
				}
			case token.CONST:
				for _, spec := range d.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						fileMap.Constants = append(fileMap.Constants, valueSpec)
					}
				}
			case token.VAR:
				for _, spec := range d.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						fileMap.Variables = append(fileMap.Variables, valueSpec)
					}
				}
			case token.TYPE:
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						if typeSpec.Assign == token.NoPos {
							fileMap.Types = append(fileMap.Types, typeSpec)
							if d.Doc != nil {
								fileMap.TypeComments[typeSpec] = d.Doc.Text()
							}
							switch typeSpec.Type.(type) {
							case *ast.StructType:
								fileMap.Structs = append(fileMap.Structs, typeSpec)
							case *ast.InterfaceType:
								fileMap.Interfaces = append(fileMap.Interfaces, typeSpec)
							}
						} else {
							fileMap.TypeAliases = append(fileMap.TypeAliases, typeSpec)
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
