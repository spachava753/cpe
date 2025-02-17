package symbolresolver

import (
	"fmt"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	"slices"
)

// extractTypeScriptSymbols extracts symbols from TypeScript source code
func extractTypeScriptSymbols(content []byte, parser *sitter.Parser) ([]string, error) {
	// Set TypeScript language for the parser
	tsLang := sitter.NewLanguage(typescript.LanguageTypescript())
	if err := parser.SetLanguage(tsLang); err != nil {
		return nil, fmt.Errorf("failed to set TypeScript language: %v", err)
	}

	// Parse the content
	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()

	symbolUsageQuery, err := sitter.NewQuery(tsLang, `(
	[
    	(import_statement
        	(import_clause
            	(named_imports
                	(import_specifier (identifier) @imported_symbols) 
                )
            )
        ) 
    ]
)`)
	if err != nil {
		return nil, fmt.Errorf("failed to create symbol usage query: %v", err)
	}
	defer symbolUsageQuery.Close()

	// Extract symbol usages
	symbolUsages := make(map[string]bool)
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	queryMatches := cursor.Matches(symbolUsageQuery, root, content)
	for match := queryMatches.Next(); match != nil; match = queryMatches.Next() {
		for _, capture := range match.Captures {
			text := capture.Node.Utf8Text(content)
			symbolUsages[text] = true
		}
	}

	// Convert symbolUsages to a sorted slice
	symbols := make([]string, 0, len(symbolUsages))
	for symbol := range symbolUsages {
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)

	// If no symbols were found, return empty slice
	if len(symbols) == 0 {
		return []string{}, nil
	}

	// Create query to find symbol definitions in other files
	var queries []string
	for _, symbol := range symbols {
		definitionQuery := fmt.Sprintf(`(
	[
    		; Class declarations
			(class_declaration
				name: (type_identifier) @name)
            
            ; Enum declarations
			(interface_declaration
				name: (type_identifier) @name)
            
            ; Enum declarations
			(enum_declaration
				name: (identifier) @name)
            
            ; Type alias declarations
			(type_alias_declaration
				name: (type_identifier) @name)
            
            ; Function declarations
			(function_declaration
				name: (identifier) @name)
            
            ; Variable declarations
			(variable_declarator
				name: (identifier) @name)
            
            ; Namespace blocks
			(internal_module
				name: (identifier) @name)
    ]
    (#any-of? @name "%s")
)`, symbol)
		queries = append(queries, definitionQuery)
	}
	return queries, nil
}
