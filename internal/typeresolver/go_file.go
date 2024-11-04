package typeresolver

import (
	"fmt"
	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	"maps"
	"slices"
	"strings"
)

const typeUsageQueryStr = `
		(
			[
				(type_arguments (type_elem (type_identifier) @ignore))
				(type_declaration (type_spec name: (type_identifier) @ignore))
				(method_declaration receiver: 
					(parameter_list
						(parameter_declaration
							[
								type: (type_identifier) @ignore
								type: (pointer_type (type_identifier) @ignore) 
							]
						)
					)
				)
				(qualified_type (type_identifier) @type.usage) @qualified_type
				((type_identifier) @type.usage)
			]
			(#not-any-of? @type.usage
				"any"
				"interface{}"
				"string"
				"rune"
				"bool"
				"byte"
				"comparable"
				"complex128"
				"complex64"
				"error"
				"float32"
				"float64"
				"int16"
				"int32"
				"int64"
				"int8"
				"int"
				"uint"
				"uint16"
				"uint32"
				"uint64"
				"uint8"
				"uintptr"
			)
		)
	`

const funcUsageQueryStr = `
		(call_expression
			function: [
				(identifier) @usage
				(selector_expression
					operand: [
						(identifier) @operand_identifier
					]?
					field: (field_identifier) @usage
				)
			]
		)
	`

// extractGoSymbols extracts symbols from Go source code
func extractGoSymbols(content []byte, parser *sitter.Parser) ([]string, error) {
	// Set Go language for the parser
	goLang := sitter.NewLanguage(golang.Language())
	if err := parser.SetLanguage(goLang); err != nil {
		return nil, fmt.Errorf("failed to set Go language: %v", err)
	}

	// Parse the content
	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Create type usage query
	typeUsageQuery, err := sitter.NewQuery(goLang, typeUsageQueryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create type usage query: %v", err)
	}
	defer typeUsageQuery.Close()

	// Create function/method usage query
	funcUsageQuery, err := sitter.NewQuery(goLang, funcUsageQueryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create function usage query: %v", err)
	}
	defer funcUsageQuery.Close()

	// Extract type usages
	typeUsages := make(map[string]bool)
	ignoreTypeUsages := make(map[string]bool)
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	queryMatches := cursor.Matches(typeUsageQuery, root, content)
	for match := queryMatches.Next(); match != nil; match = queryMatches.Next() {
		for _, capture := range match.Captures {
			text := capture.Node.Utf8Text(content)
			_ = text
			if capture.Index == 0 {
				ignoreTypeUsages[capture.Node.Utf8Text(content)] = true
			}
			if !ignoreTypeUsages[capture.Node.Utf8Text(content)] && capture.Index == 1 || capture.Index == 3 { // @type.usage
				typeUsages[capture.Node.Utf8Text(content)] = true
			}
		}
	}

	// Extract function/method usages
	funcUsages := make(map[string]bool)
	queryMatches = cursor.Matches(funcUsageQuery, root, content)
	for match := queryMatches.Next(); match != nil; match = queryMatches.Next() {
		for _, capture := range match.Captures {
			if capture.Index == 0 || capture.Index == 2 { // @usage
				funcUsages[capture.Node.Utf8Text(content)] = true
			}
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
		queries = append(queries, fmt.Sprintf(`
		(type_declaration
			[
				(type_spec
					name: (type_identifier) @type.definition)
				(type_alias
					name: (type_identifier) @type.definition)
			]
			(#any-of? @type.definition "%s")
		)
	`, strings.Join(typeSymbols, `" "`)))
	}

	funcSymbols := slices.Collect(maps.Keys(funcUsages))
	for i := range len(funcSymbols) {
		funcSymbols[i] = strings.TrimSpace(funcSymbols[i])
	}
	slices.Sort(funcSymbols)

	// Construct function/method definition query
	if len(funcSymbols) > 0 {
		queries = append(queries, fmt.Sprintf(`
		(
			[
				(function_declaration
					name: (identifier) @func.definition)
				(method_declaration
					name: (field_identifier) @func.definition)
			]
			(#any-of? @func.definition "%s")
		)
`, strings.Join(funcSymbols, `" "`)))
	}

	// Return both queries
	return queries, nil
}
