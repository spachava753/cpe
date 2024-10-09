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

// GenerateOutput creates the XML-like output for the code map using AST
func GenerateOutput(fsys fs.FS, maxLiteralLen int) (string, error) {
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
		fileContent, err := generateFileOutput(fsys, path, maxLiteralLen)
		if err != nil {
			return "", fmt.Errorf("error generating output for file %s: %w", path, err)
		}
		sb.WriteString(fileContent)
	}

	sb.WriteString("</code_map>\n")
	return sb.String(), nil
}

func generateFileOutput(fsys fs.FS, path string, maxLiteralLen int) (string, error) {
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}

	parser := sitter.NewParser()
	defer parser.Close()
	err = parser.SetLanguage(sitter.NewLanguage(golang.Language()))
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
		switch node.GrammarName() {
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
				if child.GrammarName() != "block" {
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
		default:
			output.WriteString(node.Utf8Text(src))
		}
	}

	traverse(root)

	code := strings.TrimSpace(output.String())
	formattedCode, fmtErr := format.Source([]byte(code))
	if fmtErr != nil {
		return "", fmt.Errorf("error formatting code: %w", fmtErr)
	}

	return fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n%s</file_map>\n</file>\n", path, formattedCode), nil
}

func traverseAndTruncate(node *sitter.Node, src []byte, output *strings.Builder, maxLiteralLen int) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		cchild := child.Utf8Text(src)
		_ = cchild
		tchild := child.GrammarName()
		_ = tchild
		switch child.GrammarName() {
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
			if node.GrammarName() == "var_spec_list" ||
				(node.GrammarName() == "const_declaration" && node.NamedChildCount() > 0) {
				output.WriteString("\n")
			}
		case "=":
			output.WriteString(child.Utf8Text(src))
			output.WriteString(" ")
		case "var", "const":
			output.WriteString(child.Utf8Text(src))
			output.WriteString(" ")
		case "identifier":
			for p := node; p != nil; p = p.Parent() {
				if p.GrammarName() == "var_spec_list" ||
					(p.GrammarName() == "const_declaration" && p.NamedChildCount() > 1) {
					output.WriteString("\t")
					break
				}
			}
			output.WriteString(child.Utf8Text(src))
			output.WriteString(" ")
		case "comment":
			for p := node; p != nil; p = p.Parent() {
				if p.GrammarName() == "var_spec_list" ||
					(p.GrammarName() == "const_declaration" && p.NamedChildCount() > 1) {
					output.WriteString("\t")
					break
				}
			}
			output.WriteString(child.Utf8Text(src))
			if child.PrevNamedSibling() == nil || child.PrevNamedSibling().EndPosition().Row != child.EndPosition().Row {
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
