package symbolresolver

import (
	"fmt"
	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	"maps"
	"slices"
	"strings"
)

const pythonTypeUsageQueryStr = `(
	[
		(class_definition
			superclasses: (argument_list
				[
					(identifier) @type.usage
					(attribute
						object: (identifier) @type.usage
						attribute: (identifier))
				]
			)
		)
		(type
			[
				(identifier) @type.usage
				(generic_type (identifier) @type.usage)
			]
		)
		(subscript
			value: [
				(identifier) @type.usage
				(attribute
					object: (identifier) @type.usage
					attribute: (identifier))
			]
		)
		(call
			function: (identifier) @type.usage)
		(import_from_statement
			name: (dotted_name) @type.usage)
	]
	(#not-any-of? @type.usage
	"str"
	"int"
	"float"
	"bool"
	"bytes"
	"None"
	"Any"
	"List"
	"Dict"
	"Set"
	"Tuple"
	"Optional"
	"Union"
	"Callable"
	"Type"
	"TypeVar"
	"Generic"
	"Protocol"
	"ABC"
	"abstractmethod"
	"dataclass"
	"typing"
	"abc"
	"dataclasses"
	"P"
	"R"
	"T"
	"pkg"
	"models"
	"base"
	"utils"
	"self"
	"cls"
	"object"
	"print"
	)
)`

const pythonFuncUsageQueryStr = `(
	[
		(call
			function: [
				(identifier) @usage
				(attribute
					object: (identifier)
					attribute: (identifier) @usage
				)
			]
		)
		(decorator
			[
				(identifier) @usage
				(attribute
					object: (identifier)
					attribute: (identifier) @usage)
			]
		)
	]
	(#not-any-of? @usage
	"str"
	"int"
	"float"
	"bool"
	"bytes"
	"None"
	"Any"
	"List"
	"Dict"
	"Set"
	"Tuple"
	"Optional"
	"Union"
	"Callable"
	"Type"
	"TypeVar"
	"Generic"
	"Protocol"
	"ABC"
	"abstractmethod"
	"dataclass"
	"typing"
	"abc"
	"dataclasses"
	"P"
	"R"
	"T"
	"pkg"
	"models"
	"base"
	"utils"
	"self"
	"cls"
	"object"
	"print"
	)
)`

// extractPythonSymbols extracts symbols from Python source code
func extractPythonSymbols(content []byte, parser *sitter.Parser) ([]string, error) {
	// Set Python language for the parser
	pythonLang := sitter.NewLanguage(python.Language())
	if err := parser.SetLanguage(pythonLang); err != nil {
		return nil, fmt.Errorf("failed to set Python language: %v", err)
	}

	// Parse the content
	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Create type usage query
	typeUsageQuery, err := sitter.NewQuery(pythonLang, pythonTypeUsageQueryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create type usage query: %v", err)
	}
	defer typeUsageQuery.Close()

	// Create function usage query
	funcUsageQuery, err := sitter.NewQuery(pythonLang, pythonFuncUsageQueryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create function usage query: %v", err)
	}
	defer funcUsageQuery.Close()

	// Extract type usages
	typeUsages := make(map[string]bool)
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	queryMatches := cursor.Matches(typeUsageQuery, root, content)
	for match := queryMatches.Next(); match != nil; match = queryMatches.Next() {
		for _, capture := range match.Captures {
			text := capture.Node.Utf8Text(content)
			typeUsages[text] = true
		}
	}

	// Extract function usages
	funcUsages := make(map[string]bool)
	queryMatches = cursor.Matches(funcUsageQuery, root, content)
	for match := queryMatches.Next(); match != nil; match = queryMatches.Next() {
		for _, capture := range match.Captures {
			text := capture.Node.Utf8Text(content)
			funcUsages[text] = true
		}
	}

	queries := make([]string, 0, 2)

	typeSymbols := slices.Collect(maps.Keys(typeUsages))
	for i := range len(typeSymbols) {
		typeSymbols[i] = strings.TrimSpace(typeSymbols[i])
	}
	slices.Sort(typeSymbols)

	// Construct type definition query
	if len(typeSymbols) > 0 {
		queries = append(queries, fmt.Sprintf(`(
	[
	(class_definition
		name: (identifier) @type.definition)
	(decorated_definition
		definition: (class_definition
			name: (identifier) @type.definition))
	]
	(#any-of? @type.definition "%s")
)`, strings.Join(typeSymbols, `" "`)))
	}

	funcSymbols := slices.Collect(maps.Keys(funcUsages))
	for i := range len(funcSymbols) {
		funcSymbols[i] = strings.TrimSpace(funcSymbols[i])
	}
	slices.Sort(funcSymbols)

	// Construct function definition query
	if len(funcSymbols) > 0 {
		queries = append(queries, fmt.Sprintf(`(
	[
		(function_definition
			name: (identifier) @func.definition)
		(decorated_definition
			definition: (function_definition
				name: (identifier) @func.definition))
	]
	(#any-of? @func.definition "%s")
)`, strings.Join(funcSymbols, `" "`)))
	}

	// Return both queries
	return queries, nil
}
