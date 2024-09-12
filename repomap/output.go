package repomap

import (
	"fmt"
	"go/ast"
	"strings"
)

// GenerateOutput creates the XML-like output for the repo map
func (r *RepoMap) GenerateOutput() string {
	var sb strings.Builder

	sb.WriteString("<repo_map>\n")
	for _, file := range r.Files {
		sb.WriteString(file.generateOutput())
	}
	sb.WriteString("</repo_map>\n")

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

	for _, s := range f.Structs {
		if comment, ok := f.StructComments[s]; ok {
			sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(comment)))
		}
		typeParams := ""
		if s.TypeParams != nil {
			params := make([]string, len(s.TypeParams.List))
			for i, param := range s.TypeParams.List {
				params[i] = param.Names[0].Name + " any"
			}
			typeParams = fmt.Sprintf("[%s]", strings.Join(params, ", "))
		}
		sb.WriteString(fmt.Sprintf("type %s%s struct {\n", s.Name.Name, typeParams))
		if structType, ok := s.Type.(*ast.StructType); ok {
			for _, field := range structType.Fields.List {
				if comment, ok := f.FieldComments[field]; ok {
					sb.WriteString(fmt.Sprintf("    %s\n", prependCommentSlashes(comment)))
				}
				fieldNames := make([]string, len(field.Names))
				for i, name := range field.Names {
					fieldNames[i] = name.Name
				}
				sb.WriteString(fmt.Sprintf("    %s %s\n", strings.Join(fieldNames, ", "), fieldType(field.Type)))
			}
		}
		sb.WriteString("}\n")
	}

	for _, i := range f.Interfaces {
		sb.WriteString(fmt.Sprintf("type %s interface {\n", i.Name.Name))
		if interfaceType, ok := i.Type.(*ast.InterfaceType); ok {
			for _, method := range interfaceType.Methods.List {
				if len(method.Names) > 0 {
					sb.WriteString(fmt.Sprintf("    %s%s\n", method.Names[0].Name, funcType(method.Type)))
				}
			}
		}
		sb.WriteString("}\n")
	}

	for _, fn := range f.Functions {
		if comment, ok := f.Comments[fn]; ok {
			sb.WriteString(fmt.Sprintf("%s\n", prependCommentSlashes(comment)))
		}
		sb.WriteString(fmt.Sprintf("func %s%s\n", fn.Name.Name, funcType(fn.Type)))
	}

	for _, methods := range f.Methods {
		for _, method := range methods {
			receiver := method.Recv.List[0]
			recvType := fieldType(receiver.Type)
			recvName := ""
			if len(receiver.Names) > 0 {
				recvName = receiver.Names[0].Name
			}
			sb.WriteString(fmt.Sprintf("func (%s %s) %s%s\n", recvName, recvType, method.Name.Name, funcType(method.Type)))
		}
	}

	sb.WriteString("</file_map>\n</file>\n")

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
		if valueType == "ast.InterfaceType" {
			valueType = "interface{}"
		}
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
