package codemap

import (
	"fmt"
	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	"go/format"
	"sort"
	"strings"
)

func generateGoFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	goLang := sitter.NewLanguage(golang.Language())
	err := parser.SetLanguage(goLang)
	if err != nil {
		return "", err
	}

	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Queries for function and method declarations
	funcQuery, queryErr := sitter.NewQuery(goLang, `
		(function_declaration
			name: (identifier) @func.name
			body: (block) @func.body)
	`)
	if queryErr != nil {
		return "", convertQueryError("function query", queryErr)
	}
	defer funcQuery.Close()

	methodQuery, queryErr := sitter.NewQuery(goLang, `
		(method_declaration
			name: (field_identifier) @method.name
			body: (block) @method.body)
	`)
	if queryErr != nil {
		return "", convertQueryError("method query", queryErr)
	}
	defer methodQuery.Close()

	// Query for string literals
	stringLiteralQuery, queryErr := sitter.NewQuery(goLang, `
			(interpreted_string_literal) @string
			(raw_string_literal) @string
	`)
	if queryErr != nil {
		return "", convertQueryError("string literal query", queryErr)
	}
	defer stringLiteralQuery.Close()

	// Execute queries
	funcCursor := sitter.NewQueryCursor()
	defer funcCursor.Close()
	funcMatches := funcCursor.Matches(funcQuery, root, src)

	methodCursor := sitter.NewQueryCursor()
	defer methodCursor.Close()
	methodMatches := methodCursor.Matches(methodQuery, root, src)

	stringLiteralCursor := sitter.NewQueryCursor()
	defer stringLiteralCursor.Close()
	stringLiteralMatches := stringLiteralCursor.Matches(stringLiteralQuery, root, src)

	// Collect positions to cut
	type cutRange struct {
		start, end  uint
		addEllipsis bool
	}
	cutRanges := make([]cutRange, 0)

	// Collect function and method body ranges
	bodyRanges := make([]cutRange, 0)

	for match := funcMatches.Next(); match != nil; match = funcMatches.Next() {
		for _, capture := range match.Captures {
			if capture.Node.Kind() == "block" {
				bodyRanges = append(bodyRanges, cutRange{
					start:       capture.Node.StartByte(),
					end:         capture.Node.EndByte(),
					addEllipsis: false,
				})
			}
		}
	}

	for match := methodMatches.Next(); match != nil; match = methodMatches.Next() {
		for _, capture := range match.Captures {
			if capture.Node.Kind() == "block" {
				bodyRanges = append(bodyRanges, cutRange{
					start:       capture.Node.StartByte(),
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

			// Check if the string literal is within a function or method body
			inBody := false
			for _, bodyRange := range bodyRanges {
				if start >= bodyRange.start && end <= bodyRange.end {
					inBody = true
					break
				}
			}

			str := strings.Trim(content, "\"`")
			quoteLen := (len(content) - len(str)) / 2
			if !inBody && len(str) > maxLiteralLen {
				cutRanges = append(cutRanges, cutRange{
					start:       start + uint(maxLiteralLen) + uint(quoteLen), // +quoteLen to keep the starting quotes
					end:         end - uint(quoteLen),                         // -quoteLen to keep the closing quotes
					addEllipsis: true,
				})
			}
		}
	}

	// Add function and method body ranges to cutRanges
	cutRanges = append(cutRanges, bodyRanges...)

	// Sort cutRanges by start position
	sort.Slice(cutRanges, func(i, j int) bool {
		return cutRanges[i].start < cutRanges[j].start
	})

	// Create new source with truncated string literals and without function bodies
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

	code := strings.TrimSpace(string(newSrc))
	formattedCode, fmtErr := format.Source([]byte(code))
	if fmtErr != nil {
		return "", fmt.Errorf("error formatting code: %w", fmtErr)
	}

	return strings.TrimSpace(string(formattedCode)), nil
}
