package codemap

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func generateTypeScriptFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	tsLang := sitter.NewLanguage(typescript.LanguageTypescript())
	err := parser.SetLanguage(tsLang)
	if err != nil {
		return "", err
	}

	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Query for function and method bodies
	methodQuery, queryErr := sitter.NewQuery(tsLang, `
		[
			(method_definition
				body: (statement_block) @method.body)
			(function_declaration
				body: (statement_block) @func.body)
			(arrow_function
				body: [
					(statement_block) @arrow.body
					(expression) @arrow.expr
				])
		]
	`)
	if queryErr != nil {
		return "", convertQueryError("method query", queryErr)
	}
	defer methodQuery.Close()

	// Query for string literals
	stringLiteralQuery, queryErr := sitter.NewQuery(tsLang, `
		[
			(string) @string
			(template_string) @string
		]
	`)
	if queryErr != nil {
		return "", convertQueryError("string literal query", queryErr)
	}
	defer stringLiteralQuery.Close()

	// Execute queries
	methodCursor := sitter.NewQueryCursor()
	defer methodCursor.Close()
	methodMatches := methodCursor.Matches(methodQuery, root, src)

	stringLiteralCursor := sitter.NewQueryCursor()
	defer stringLiteralCursor.Close()
	stringLiteralMatches := stringLiteralCursor.Matches(stringLiteralQuery, root, src)

	// Collect positions to cut
	cutRanges := make([]transformation, 0)

	// Collect method and function body ranges
	for match := methodMatches.Next(); match != nil; match = methodMatches.Next() {
		for _, capture := range match.Captures {
			if strings.HasSuffix(capture.Node.Kind(), "_block") || capture.Node.Kind() == "expression" {
				// Just remove the body without adding a replacement
				cutRanges = append(cutRanges, transformation{
					cutStart:    capture.Node.StartByte(),
					cutEnd:      capture.Node.EndByte(),
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

			str := strings.Trim(content, "\"`'")
			quoteLen := (len(content) - len(str)) / 2
			if len(str) > maxLiteralLen {
				cutRanges = append(cutRanges, transformation{
					cutStart:    start + uint(maxLiteralLen) + uint(quoteLen),
					cutEnd:      end - uint(quoteLen),
					addEllipsis: true,
				})
			}
		}
	}

	// Sort cutRanges by cutStart position
	sort.Slice(cutRanges, func(i, j int) bool {
		return cutRanges[i].cutStart < cutRanges[j].cutStart
	})

	// Remove subset ranges and check for overlaps
	filteredRanges, err := collapseTransformations(cutRanges)
	if err != nil {
		return "", err
	}

	// Create new source with truncated string literals and without method bodies
	var newSrc []byte
	lastEnd := uint(0)
	for _, r := range filteredRanges {
		newSrc = append(newSrc, src[lastEnd:r.cutStart]...)
		if r.prependText != "" {
			newSrc = append(newSrc, []byte(r.prependText)...)
		}
		if r.addEllipsis {
			newSrc = append(newSrc, []byte("...")...)
		}
		lastEnd = r.cutEnd
	}
	newSrc = append(newSrc, src[lastEnd:]...)

	// Clean up extra whitespace
	output := string(newSrc)
	return strings.TrimSpace(output), nil
}
