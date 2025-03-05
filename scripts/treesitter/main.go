package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func getLanguage(lang string) (*sitter.Language, error) {
	switch strings.ToLower(lang) {
	case "go":
		return sitter.NewLanguage(golang.Language()), nil
	case "java":
		return sitter.NewLanguage(java.Language()), nil
	case "js", "javascript":
		return sitter.NewLanguage(javascript.Language()), nil
	case "py", "python":
		return sitter.NewLanguage(python.Language()), nil
	case "ts", "typescript":
		return sitter.NewLanguage(typescript.LanguageTypescript()), nil
	default:
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
}

func printNode(node *sitter.Node, source []byte, indent int) {
	// Print the current node with indentation
	indentStr := strings.Repeat("  ", indent)
	nodeType := node.Kind()
	startPoint := node.StartPosition()
	endPoint := node.EndPosition()

	// If node has a field name (is a named child), include it
	if node.Parent() != nil {
		for i := uint32(0); i < uint32(node.Parent().NamedChildCount()); i++ {
			if node.Parent().NamedChild(uint(i)) == node {
				field := node.Parent().FieldNameForChild(i)
				if field != "" {
					nodeType = fmt.Sprintf("%s: %s", field, nodeType)
				}
				break
			}
		}
	}

	fmt.Printf("%s%s [%d, %d] - [%d, %d]\n",
		indentStr,
		nodeType,
		startPoint.Row,
		startPoint.Column,
		endPoint.Row,
		endPoint.Column,
	)

	// Recursively print child nodes
	for i := uint32(0); i < uint32(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child != nil {
			printNode(child, source, indent+1)
		}
	}
}

func main() {
	// Define command line flags
	langFlag := flag.String("lang", "", "Language to parse (go, java, js, py, ts)")
	flag.Parse()

	if *langFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: language flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Get the language
	lang, err := getLanguage(*langFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Read source code from stdin
	reader := bufio.NewReader(os.Stdin)
	var source []byte
	for {
		chunk, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			os.Exit(1)
		}
		source = append(source, chunk...)
		if err == io.EOF {
			break
		}
	}

	// Create parser
	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(lang); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting language: %v\n", err)
		os.Exit(1)
	}

	// Parse the source code
	tree := parser.Parse(source, nil)
	defer tree.Close()

	// Print the AST
	printNode(tree.RootNode(), source, 0)
}
