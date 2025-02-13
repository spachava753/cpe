package symbolresolver

import (
	"errors"
	"fmt"
	gitignore "github.com/sabhiram/go-gitignore"
	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	"io/fs"
	"path/filepath"
	"strings"
)

var errUnknownExt = errors.New("unsupported file extension")

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
		return nil, errUnknownExt
	}
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
		lang = sitter.NewLanguage(python.Language())
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
func ResolveTypeAndFunctionFiles(selectedFiles []string, sourceFS fs.FS, ignorer *gitignore.GitIgnore) (map[string]bool, error) {
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
			if !errors.Is(err, errUnknownExt) {
				return nil, fmt.Errorf("failed to extract symbols from %s: %v", file, err)
			}
			fmt.Printf("failed to extract symbols from %s: %v\n", file, err)
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
		if d.IsDir() || result[path] || (ignorer != nil && ignorer.MatchesPath(path)) {
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
