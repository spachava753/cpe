package symbolresolver

import (
	"fmt"
	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	"slices"
)

// extractJavaScriptSymbols extracts symbols from JavaScript source code
func extractJavaScriptSymbols(content []byte, parser *sitter.Parser) ([]string, error) {
	// Set JavaScript language for the parser
	jsLang := sitter.NewLanguage(javascript.Language())
	if err := parser.SetLanguage(jsLang); err != nil {
		return nil, fmt.Errorf("failed to set JavaScript language: %v", err)
	}

	// Parse the content
	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Query to extract imported symbols from ES6 imports and CommonJS requires
	symbolUsageQuery, err := sitter.NewQuery(jsLang, `(
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
			; Function declarations
			(function_declaration
				name: (identifier) @name)
                
			; Class declarations
			(class_declaration
				name: (identifier) @name)
            
            ; Variable declarations
			(variable_declarator
				name: (identifier) @name)
			
			; CommonJS exports
			(expression_statement
            	(assignment_expression
                	[
                    	(member_expression
                    		(property_identifier) @name
                    	)
                        (function_expression
                    		(identifier) @name
                    	)
                    ]
                )
            )
    ]
    (#any-of? @name "%s")
)`, symbol)
		queries = append(queries, definitionQuery)
	}
	return queries, nil
}
