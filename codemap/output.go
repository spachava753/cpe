package codemap

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
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
	parser.SetLanguage(golang.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return "", fmt.Errorf("error parsing file: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	var output strings.Builder

	// Traverse the tree and extract relevant information
	var traverse func(node *sitter.Node)
	traverse = func(node *sitter.Node) {
		switch node.Type() {
		case "source_file":
			for i := 0; i < int(node.NamedChildCount()); i++ {
				traverse(node.NamedChild(i))
			}
		case "function_declaration", "method_declaration":
			// Extract only the signature for functions and methods
			output.WriteString("func ")
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				cName := node.FieldNameForChild(i + 1)
				if child.Type() != "block" {
					output.WriteString(child.Content(src))
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
			output.WriteString(node.Content(src))
			output.WriteString("\n")
		default:
			output.WriteString(node.Content(src))
			output.WriteString("\n\n")
		}
	}

	traverse(root)

	return fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n%s\n</file_map>\n</file>\n", path, strings.TrimSpace(output.String())), nil
}

func traverseAndTruncate(node *sitter.Node, src []byte, output *strings.Builder, maxLiteralLen int) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		cchild := child.Content(src)
		_ = cchild
		tchild := child.Type()
		_ = tchild
		switch child.Type() {
		case "interpreted_string_literal", "raw_string_literal":
			content := child.Content(src)
			if len(content)-2 > maxLiteralLen {
				truncated := content[:maxLiteralLen+1] + "..." + content[len(content)-1:]
				output.WriteString(truncated)
			} else {
				output.WriteString(content)
			}
		case "(", ")":
			output.WriteString(child.Content(src))
			if child.Parent().Type() == "var_spec_list" || child.Parent().Type() == "const_spec_list" {
				output.WriteString("\n")
			}
		case "=":
			output.WriteString(child.Content(src))
			output.WriteString(" ")
		case "var":
			output.WriteString(child.Content(src))
			output.WriteString(" ")
		case "identifier":
			for p := child.Parent(); p != nil; p = p.Parent() {
				if p.Type() == "var_spec_list" || p.Type() == "const_spec_list" {
					output.WriteString("\t")
					break
				}
			}
			output.WriteString(child.Content(src))
			output.WriteString(" ")
		default:
			if child.ChildCount() > 0 {
				traverseAndTruncate(child, src, output, maxLiteralLen)
			} else {
				output.WriteString(child.Content(src))
			}
		}
	}
}
