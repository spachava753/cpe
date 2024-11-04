package typeresolver

import (
	"fmt"
	"github.com/spachava753/cpe/internal/ignore"
	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// getFileExtension returns the file extension without the dot
func getFileExtension(path string) string {
	return strings.TrimPrefix(filepath.Ext(path), ".")
}

// extractSymbolsAndCreateQueries extracts symbols from a file and creates tree-sitter queries
func extractSymbolsAndCreateQueries(content []byte, ext string, parser *sitter.Parser) ([]string, error) {
	switch ext {
	case "go":
		return extractGoSymbols(content, parser)
	case "java":
		return extractJavaSymbols(content, parser)
	case "py":
		return extractPythonSymbols(content, parser)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}
}

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
	typeUsageQuery, err := sitter.NewQuery(goLang, `
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
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create type usage query: %v", err)
	}
	defer typeUsageQuery.Close()

	// Create function/method usage query
	funcUsageQuery, err := sitter.NewQuery(goLang, `
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
	`)
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

// extractJavaSymbols extracts symbols from Java source code
func extractJavaSymbols(content []byte, parser *sitter.Parser) ([]string, error) {
	// TODO: Implement Java-specific symbol extraction
	// The query should look for:
	// - Class references in extends/implements clauses
	// - Type usage in method parameters, return types, variable declarations
	// - Generic type parameters
	// - Annotation types
	return []string{}, nil
}

// extractPythonSymbols extracts symbols from Python source code
func extractPythonSymbols(content []byte, parser *sitter.Parser) ([]string, error) {
	// TODO: Implement Python-specific symbol extraction
	// The query should look for:
	// - Type hints in function parameters and return types
	// - Class inheritance
	// - Type annotations in variable declarations
	// - Import statements
	return []string{}, nil
}

// runQueriesOnFile runs the provided queries on a file and returns true if any match is found
func runQueriesOnFile(content []byte, queries []string, ext string, parser *sitter.Parser) (bool, error) {
	// Set the appropriate language based on file extension
	var lang *sitter.Language
	switch ext {
	case "go":
		lang = sitter.NewLanguage(golang.Language())
	case "java":
		// TODO: Add Java language support when implemented
		return false, fmt.Errorf("java support not yet implemented")
	case "py":
		// TODO: Add Python language support when implemented
		return false, fmt.Errorf("python support not yet implemented")
	default:
		return false, fmt.Errorf("unsupported file extension: %s", ext)
	}

	// Set the language for the parser
	if err := parser.SetLanguage(lang); err != nil {
		return false, fmt.Errorf("failed to set language for %s: %v", ext, err)
	}

	// Parse the content
	tree := parser.Parse(content, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Create a query cursor that will be reused for all queries
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	// Run each query
	for _, queryStr := range queries {
		// Create a new query
		query, err := sitter.NewQuery(lang, queryStr)
		if err != nil {
			return false, fmt.Errorf("failed to create query: %v", err)
		}
		defer query.Close()

		// Execute the query
		queryMatches := cursor.Matches(query, root, content)

		// Check if there are any matches
		if match := queryMatches.Next(); match != nil && len(match.Captures) > 0 {
			return true, nil
		}
	}

	return false, nil
}

// ResolveTypeAndFunctionFiles resolves all type and function definitions used in the given files.
func ResolveTypeAndFunctionFiles(selectedFiles []string, sourceFS fs.FS, ignoreRules *ignore.Patterns) (map[string]bool, error) {
	// Map to store queries grouped by file extension
	// The inner map stores unique queries to prevent duplicates
	queriesByExt := make(map[string]map[string]bool)

	// Map to store the result (files that need to be included)
	result := make(map[string]bool)

	// Phase 1: Extract symbols and create queries from selected files
	for _, file := range selectedFiles {
		// Add selected file to result set
		result[file] = true

		// Read file content
		content, err := fs.ReadFile(sourceFS, file)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %v", file, err)
		}

		// Get file extension
		ext := getFileExtension(file)
		if ext == "" {
			continue
		}

		// Initialize the query set for this extension if it doesn't exist
		if _, ok := queriesByExt[ext]; !ok {
			queriesByExt[ext] = make(map[string]bool)
		}

		// Create parser if needed
		parser := sitter.NewParser()
		defer parser.Close()

		// Extract symbols and create queries
		queries, err := extractSymbolsAndCreateQueries(content, ext, parser)
		if err != nil {
			return nil, fmt.Errorf("failed to extract symbols from %s: %v", file, err)
		}

		// Add queries to the map, using map[string]bool to ensure uniqueness
		for _, query := range queries {
			queriesByExt[ext][query] = true
		}
	}

	// Convert queriesByExt from map[string]map[string]bool to map[string][]string
	// This makes it easier to work with in the next phase
	consolidatedQueries := make(map[string][]string)
	for ext, querySet := range queriesByExt {
		queries := make([]string, 0, len(querySet))
		for query := range querySet {
			queries = append(queries, query)
		}
		consolidatedQueries[ext] = queries
	}

	// Phase 2: Search for definitions in all files
	// Create a parser that will be reused for all files
	parser := sitter.NewParser()
	defer parser.Close()

	err := fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip if:
		// - it's a directory
		// - it's already in the result set
		// - it should be ignored
		if d.IsDir() || result[path] || (ignoreRules != nil && ignoreRules.ShouldIgnore(path)) {
			return nil
		}

		// Get file extension
		ext := getFileExtension(path)
		if ext == "" {
			return nil
		}

		// Skip if we don't have any queries for this extension
		queries, ok := consolidatedQueries[ext]
		if !ok || len(queries) == 0 {
			return nil
		}

		// Read file content
		content, err := fs.ReadFile(sourceFS, path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %v", path, err)
		}

		// Run all queries for this extension on the file
		hasMatch, err := runQueriesOnFile(content, queries, ext, parser)
		if err != nil {
			return fmt.Errorf("failed to run queries on %s: %v", path, err)
		}

		// If we found a match, add the file to the result set
		if hasMatch {
			result[path] = true
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk source directory: %v", err)
	}

	return result, nil
}
