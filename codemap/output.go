package codemap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// GenerateOutput creates the XML-like output for the code map
func (r *CodeMap) GenerateOutput() string {
	var sb strings.Builder

	sb.WriteString("<code_map>\n")
	for _, file := range r.Files {
		sb.WriteString(file.generateOutput())
	}
	sb.WriteString("</code_map>\n")

	return sb.String()
}

func (f *FileMap) generateOutput() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n", f.Path))
	if f.PackageComment != "" {
		sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(f.PackageComment)))
	}
	sb.WriteString(fmt.Sprintf("package %s\n", f.PackageName))

	if len(f.Imports) > 0 {
		sb.WriteString("import (\n")
		for _, imp := range f.Imports {
			sb.WriteString(fmt.Sprintf(" %s\n", imp.Path.Value))
		}
		sb.WriteString(")\n")
	}

	// Generate output for constants, variables, types, and functions in the order they appear
	for _, item := range f.Declarations {
		switch v := item.(type) {
		case *ast.GenDecl:
			switch v.Tok {
			case token.CONST, token.VAR:
				sb.WriteString(genDeclString(v, f.Comments))
			case token.TYPE:
				sb.WriteString(typeString(v, f.TypeComments, f.FieldComments))
			}
		case *ast.FuncDecl:
			sb.WriteString(funcString(v, f.Comments))
		}
	}

	sb.WriteString("</file_map>\n</file>\n")

	return sb.String()
}

func genDeclString(d *ast.GenDecl, comments map[ast.Node]string) string {
	var sb strings.Builder
	if comment, ok := comments[d]; ok {
		sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(comment)))
	}
	sb.WriteString(fmt.Sprintf("%s ", d.Tok.String()))
	if d.Lparen != 0 {
		sb.WriteString("(\n")
		for _, spec := range d.Specs {
			sb.WriteString(fmt.Sprintf(" %s\n", valueSpecString(spec.(*ast.ValueSpec))))
		}
		sb.WriteString(")\n")
	} else {
		sb.WriteString(fmt.Sprintf("%s\n", valueSpecString(d.Specs[0].(*ast.ValueSpec))))
	}
	return sb.String()
}

func valueSpecString(v *ast.ValueSpec) string {
	var sb strings.Builder
	for i, name := range v.Names {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name.Name)
	}
	if v.Type != nil {
		sb.WriteString(" ")
		sb.WriteString(fieldType(v.Type))
	}
	if len(v.Values) > 0 {
		sb.WriteString(" = ")
		for i, value := range v.Values {
			if i > 0 {
				sb.WriteString(", ")
			}
			switch val := value.(type) {
			case *ast.BasicLit:
				sb.WriteString(val.Value)
			case *ast.Ident:
				sb.WriteString(val.Name)
			default:
				sb.WriteString(fmt.Sprintf("%#v", value))
			}
		}
	}
	return sb.String()
}

func fieldType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", t.X, t.Sel.Name)
	case *ast.StarExpr:
		return "*" + fieldType(t.X)
	case *ast.ArrayType:
		return "[]" + fieldType(t.Elt)
	case *ast.MapType:
		keyType := fieldType(t.Key)
		valueType := fieldType(t.Value)
		return fmt.Sprintf("map[%s]%s", keyType, valueType)
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func prependCommentSlashes(comment string) string {
	lines := strings.Split(strings.TrimSpace(comment), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			lines[i] = "// " + line
		}
	}
	return strings.Join(lines, "\n")
}

func typeString(d *ast.GenDecl, typeComments map[*ast.TypeSpec]string, fieldComments map[*ast.Field]string) string {
	var sb strings.Builder
	for _, spec := range d.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			if typeSpec.Assign == token.NoPos {
				if comment, ok := typeComments[typeSpec]; ok {
					sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(comment)))
				}
			}
			sb.WriteString(fmt.Sprintf("type %s", typeSpec.Name.Name))
			if typeSpec.TypeParams != nil {
				sb.WriteString("[")
				for i, param := range typeSpec.TypeParams.List {
					if i > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(fmt.Sprintf("%s any", param.Names[0].Name))
				}
				sb.WriteString("]")
			}
			if typeSpec.Assign != 0 {
				sb.WriteString(" = ")
			} else {
				sb.WriteString(" ")
			}
			switch t := typeSpec.Type.(type) {
			case *ast.StructType:
				sb.WriteString("struct {\n")
				for _, field := range t.Fields.List {
					if comment, ok := fieldComments[field]; ok {
						sb.WriteString(fmt.Sprintf("    %s\n", prependCommentSlashes(comment)))
					}
					sb.WriteString(fmt.Sprintf("    %s\n", fieldString(field)))
				}
				sb.WriteString("}")
			case *ast.InterfaceType:
				sb.WriteString("interface {\n")
				for _, method := range t.Methods.List {
					if comment, ok := fieldComments[method]; ok {
						sb.WriteString(fmt.Sprintf("    %s\n", prependCommentSlashes(comment)))
					}
					sb.WriteString(fmt.Sprintf("    %s\n", methodString(method)))
				}
				sb.WriteString("}")
			default:
				sb.WriteString(fieldType(typeSpec.Type))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func fieldString(f *ast.Field) string {
	var names []string
	for _, name := range f.Names {
		names = append(names, name.Name)
	}
	if len(names) == 0 {
		return fieldType(f.Type)
	}
	return fmt.Sprintf("%s %s", strings.Join(names, ", "), fieldType(f.Type))
}

func methodString(m *ast.Field) string {
	if len(m.Names) > 0 {
		return fmt.Sprintf("%s%s", m.Names[0].Name, funcType(m.Type))
	}
	return fieldType(m.Type)
}

func funcString(f *ast.FuncDecl, comments map[ast.Node]string) string {
	var sb strings.Builder
	if comment, ok := comments[f]; ok {
		sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(comment)))
	}
	sb.WriteString(fmt.Sprintf("func "))
	if f.Recv != nil {
		sb.WriteString(fmt.Sprintf("(%s) ", fieldString(f.Recv.List[0])))
	}
	sb.WriteString(fmt.Sprintf("%s%s\n", f.Name.Name, funcType(f.Type)))
	return sb.String()
}

func funcType(expr ast.Expr) string {
	if ft, ok := expr.(*ast.FuncType); ok {
		var params, results []string

		if ft.Params != nil {
			for _, param := range ft.Params.List {
				paramType := fieldType(param.Type)
				if len(param.Names) > 0 {
					for _, name := range param.Names {
						params = append(params, fmt.Sprintf("%s %s", name.Name, paramType))
					}
				} else {
					params = append(params, paramType)
				}
			}
		}

		if ft.Results != nil {
			for _, result := range ft.Results.List {
				resultType := fieldType(result.Type)
				if len(result.Names) > 0 {
					for _, name := range result.Names {
						results = append(results, fmt.Sprintf("%s %s", name.Name, resultType))
					}
				} else {
					results = append(results, resultType)
				}
			}
		}

		return fmt.Sprintf("(%s) (%s)", strings.Join(params, ", "), strings.Join(results, ", "))
	}
	return ""
}

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
	var sb strings.Builder

	fset := token.NewFileSet()
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return "", err
	}

	sb.WriteString(fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n", path))

	if file.Doc != nil {
		sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(file.Doc.Text())))
	}
	sb.WriteString(fmt.Sprintf("package %s\n", file.Name.Name))

	if len(file.Imports) > 0 {
		sb.WriteString("import (\n")
		for _, imp := range file.Imports {
			sb.WriteString(fmt.Sprintf(" %s\n", imp.Path.Value))
		}
		sb.WriteString(")\n")
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			switch d.Tok {
			case token.CONST, token.VAR:
				sb.WriteString(genDeclString(d, extractComments(file)))
			case token.TYPE:
				sb.WriteString(typeString(d, extractTypeComments(file), extractFieldComments(file)))
			}
		case *ast.FuncDecl:
			sb.WriteString(funcString(d, extractComments(file)))
		}
	}

	sb.WriteString("</file_map>\n</file>\n")
	return sb.String(), nil
}

func extractComments(file *ast.File) map[ast.Node]string {
	comments := make(map[ast.Node]string)
	for _, cg := range file.Comments {
		if cg.List == nil {
			continue
		}
		lastComment := cg.List[len(cg.List)-1]
		if lastComment.End()+1 >= file.Package {
			comments[file] = cg.Text()
		} else {
			for _, decl := range file.Decls {
				if decl.Pos() > lastComment.End() {
					comments[decl] = cg.Text()
					break
				}
			}
		}
	}
	return comments
}

func extractTypeComments(file *ast.File) map[*ast.TypeSpec]string {
	typeComments := make(map[*ast.TypeSpec]string)
	for _, decl := range file.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					if typeSpec.Doc != nil {
						typeComments[typeSpec] = typeSpec.Doc.Text()
					}
				}
			}
		}
	}
	return typeComments
}

func extractFieldComments(file *ast.File) map[*ast.Field]string {
	fieldComments := make(map[*ast.Field]string)
	ast.Inspect(file, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.StructType:
			for _, field := range t.Fields.List {
				if field.Doc != nil {
					fieldComments[field] = field.Doc.Text()
				}
			}
		case *ast.InterfaceType:
			for _, method := range t.Methods.List {
				if method.Doc != nil {
					fieldComments[method] = method.Doc.Text()
				}
			}
		}
		return true
	})
	return fieldComments
}
