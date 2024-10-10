package codemap

import (
	"fmt"
	"go/format"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

var supportedLanguages = map[string]bool{
	".go":   true,
	".java": true,
	".c":    true,
	".py":   true,
	".js":   true,
	".ts":   true,
}

// GenerateOutput creates the XML-like output for the code map using AST
func GenerateOutput(fsys fs.FS, maxLiteralLen int) (string, error) {
	var sb strings.Builder
	sb.WriteString("<code_map>\n")

	var filePaths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && isSourceCode(path) {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error walking directory: %w", err)
	}

	sort.Strings(filePaths)

	for _, path := range filePaths {
		fileContent, err := generateFileOutput(fsys, path, maxLiteralLen)
		if err != nil {
			return "", fmt.Errorf("error generating output for file %s: %w", path, err)
		}
		sb.WriteString(fileContent)
	}

	sb.WriteString("</code_map>\n")
	return sb.String(), nil
}

func isSourceCode(path string) bool {
	ext := filepath.Ext(path)
	return supportedLanguages[ext]
}

func generateFileOutput(fsys fs.FS, path string, maxLiteralLen int) (string, error) {
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}

	var output string
	ext := filepath.Ext(path)

	if ext == ".go" {
		output, err = generateGoFileOutput(src, maxLiteralLen)
	} else {
		output = string(src)
	}

	if err != nil {
		return "", fmt.Errorf("error generating output for file %s: %w", path, err)
	}

	return fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n%s</file_map>\n</file>\n", path, output), nil
}

func generateGoFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	err := parser.SetLanguage(sitter.NewLanguage(golang.Language()))
	if err != nil {
		return "", err
	}

	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()
	var output strings.Builder

	// Traverse the tree and extract relevant information
	var traverse func(node *sitter.Node)
	traverse = func(node *sitter.Node) {
		switch node.Kind() {
		case "source_file":
			for i := 0; i < int(node.ChildCount()); i++ {
				traverse(node.Child(uint(i)))
			}
		case "function_declaration", "method_declaration":
			// Extract only the signature for functions and methods
			output.WriteString("func ")
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(uint(i))
				cName := node.FieldNameForChild(uint32(i + 1))
				if child.Kind() != "block" {
					output.WriteString(child.Utf8Text(src))
				}
				switch cName {
				case "receiver", "parameters":
					output.WriteString(" ")
				}
			}
			output.WriteString("\n\n")
		case "const_declaration", "var_declaration":
			traverseAndTruncate(node, src, &output, maxLiteralLen)
		case "comment":
			output.WriteString(node.Utf8Text(src))
			output.WriteString("\n")
		case "type_declaration":
			output.WriteString(node.Utf8Text(src))
			output.WriteString("\n\n")
		default:
			output.WriteString(node.Utf8Text(src))
			output.WriteString("\n")
		}
	}

	traverse(root)

	code := strings.TrimSpace(output.String())
	formattedCode, fmtErr := format.Source([]byte(code))
	if fmtErr != nil {
		return "", fmt.Errorf("error formatting code: %w", fmtErr)
	}

	return string(formattedCode), nil
}

func traverseAndTruncate(node *sitter.Node, src []byte, output *strings.Builder, maxLiteralLen int) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		cchild := child.Utf8Text(src)
		_ = cchild
		tchild := child.Kind()
		_ = tchild
		switch child.Kind() {
		case "interpreted_string_literal", "raw_string_literal":
			content := child.Utf8Text(src)
			if len(content)-2 > maxLiteralLen {
				truncated := content[:maxLiteralLen+1] + "..." + content[len(content)-1:]
				output.WriteString(truncated)
			} else {
				output.WriteString(content)
			}
		case "(":
			output.WriteString(child.Utf8Text(src))
			if node.Kind() == "var_spec_list" ||
				(node.Kind() == "const_declaration" && node.NamedChildCount() > 0) {
				output.WriteString("\n")
			}
		case ")":
			if node.Kind() == "var_spec_list" ||
				(node.Kind() == "const_declaration" && node.NamedChildCount() > 0) {
				output.WriteString("\n)\n")
			} else {
				output.WriteString(")")
			}
		case "=":
			output.WriteString(child.Utf8Text(src))
			output.WriteString(" ")
		case "var", "const":
			output.WriteString(child.Utf8Text(src))
			output.WriteString(" ")
		case "identifier":
			for p := node; p != nil; p = p.Parent() {
				if p.Kind() == "var_spec_list" ||
					(p.Kind() == "const_declaration" && p.NamedChildCount() > 1) {
					output.WriteString("\t")
					break
				}
			}
			output.WriteString(child.Utf8Text(src))
			output.WriteString(" ")
		case "comment":
			for p := node; p != nil; p = p.Parent() {
				if p.Kind() == "var_spec_list" ||
					(p.Kind() == "const_declaration" && p.NamedChildCount() > 1) {
					output.WriteString("\t")
					break
				}
			}
			output.WriteString(child.Utf8Text(src))
			if child.PrevNamedSibling() == nil || child.PrevNamedSibling().EndPosition().Row != child.EndPosition().Row {
				output.WriteString("\n")
			}
		case "var_spec", "const_spec":
			traverseAndTruncate(child, src, output, maxLiteralLen)
			if child.NextNamedSibling() == nil || child.NextNamedSibling().StartPosition().Row > child.StartPosition().Row {
				output.WriteString("\n")
			}
		default:
			if child.ChildCount() > 0 {
				traverseAndTruncate(child, src, output, maxLiteralLen)
			} else {
				output.WriteString(child.Utf8Text(src))
			}
		}
	}
}
