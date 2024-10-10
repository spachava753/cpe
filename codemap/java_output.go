package codemap

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

func generateJavaFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	javaLang := sitter.NewLanguage(java.Language())
	err := parser.SetLanguage(javaLang)
	if err != nil {
		return "", err
	}

	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Queries for method and constructor declarations
	methodQuery, queryErr := sitter.NewQuery(javaLang, `
		(method_declaration
			name: (identifier) @method.name
			body: (block) @method.body)
	`)
	if queryErr != nil {
		return "", convertQueryError("method query", queryErr)
	}
	defer methodQuery.Close()

	constructorQuery, queryErr := sitter.NewQuery(javaLang, `
		(constructor_declaration
			name: (identifier) @constructor.name
			body: (constructor_body) @constructor.body)
	`)
	if queryErr != nil {
		return "", convertQueryError("constructor query", queryErr)
	}
	defer constructorQuery.Close()

	// Query for string literals
	stringLiteralQuery, queryErr := sitter.NewQuery(javaLang, `
		(string_literal) @string
	`)
	if queryErr != nil {
		return "", convertQueryError("string literal query", queryErr)
	}
	defer stringLiteralQuery.Close()

	// Execute queries
	methodCursor := sitter.NewQueryCursor()
	defer methodCursor.Close()
	methodMatches := methodCursor.Matches(methodQuery, root, src)

	constructorCursor := sitter.NewQueryCursor()
	defer constructorCursor.Close()
	constructorMatches := constructorCursor.Matches(constructorQuery, root, src)

	stringLiteralCursor := sitter.NewQueryCursor()
	defer stringLiteralCursor.Close()
	stringLiteralMatches := stringLiteralCursor.Matches(stringLiteralQuery, root, src)

	// Collect positions to cut
	type cutRange struct {
		start, end  uint
		addEllipsis bool
	}
	cutRanges := make([]cutRange, 0)

	// Collect class, method, and constructor body ranges
	bodyRanges := make([]cutRange, 0)

	for match := methodMatches.Next(); match != nil; match = methodMatches.Next() {
		for _, capture := range match.Captures {
			if capture.Node.Kind() == "block" {
				bodyRanges = append(bodyRanges, cutRange{
					start:       capture.Node.StartByte() - 1,
					end:         capture.Node.EndByte(),
					addEllipsis: false,
				})
			}
		}
	}

	for match := constructorMatches.Next(); match != nil; match = constructorMatches.Next() {
		for _, capture := range match.Captures {
			if capture.Node.Kind() == "constructor_body" {
				bodyRanges = append(bodyRanges, cutRange{
					start:       capture.Node.StartByte() - 1,
					end:         capture.Node.EndByte(),
					addEllipsis: false,
				})
			}
		}
	}

	// Collect string literal ranges
	for match := stringLiteralMatches.Next(); match != nil; match = stringLiteralMatches.Next() {
		for _, capture := range match.Captures {
			start := capture.Node.StartByte()
			end := capture.Node.EndByte()
			content := string(src[start:end])

			// Check if the string literal is within a class, method, or constructor body
			inBody := false
			for _, bodyRange := range bodyRanges {
				if start >= bodyRange.start && end <= bodyRange.end {
					inBody = true
					break
				}
			}

			str := strings.Trim(content, "\"")
			quoteLen := (len(content) - len(str)) / 2
			if !inBody && len(str) > maxLiteralLen {
				cutRanges = append(cutRanges, cutRange{
					start:       start + uint(maxLiteralLen) + uint(quoteLen), // +1 to keep the starting quote
					end:         end - uint(quoteLen),                         // -quoteLen to keep the closing quote
					addEllipsis: true,
				})
			}
		}
	}

	// Add class, method, and constructor body ranges to cutRanges
	cutRanges = append(cutRanges, bodyRanges...)

	// Sort cutRanges by start position
	sort.Slice(cutRanges, func(i, j int) bool {
		return cutRanges[i].start < cutRanges[j].start
	})

	// Create new source with truncated string literals and without method bodies
	var newSrc []byte
	lastEnd := uint(0)
	for _, r := range cutRanges {
		newSrc = append(newSrc, src[lastEnd:r.start]...)
		if r.addEllipsis {
			newSrc = append(newSrc, []byte("...")...)
		}
		lastEnd = r.end
	}
	newSrc = append(newSrc, src[lastEnd:]...)

	return strings.TrimSpace(string(newSrc)), nil
}
