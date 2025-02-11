package codemap

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript"
)

func generateTypeScriptFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	tsLang := sitter.NewLanguage(typescript.TypeScript())
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
			(class_declaration
				body: (class_body) @class.body)
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
	type cutRange struct {
		start, end  uint
		addEllipsis bool
	}
	cutRanges := make([]cutRange, 0)

	// Collect method and function body ranges
	for match := methodMatches.Next(); match != nil; match = methodMatches.Next() {
		for _, capture := range match.Captures {
			switch capture.Node.Kind() {
			case "statement_block", "expression":
				cutRanges = append(cutRanges, cutRange{
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

			// Check if the string literal is within a method or function body
			inBody := false
			for _, bodyRange := range cutRanges {
				if start >= bodyRange.start && end <= bodyRange.end {
					inBody = true
					break
				}
			}

			str := strings.Trim(content, "\"`'")
			quoteLen := (len(content) - len(str)) / 2
			if !inBody && len(str) > maxLiteralLen {
				cutRanges = append(cutRanges, cutRange{
					start:       start + uint(maxLiteralLen) + uint(quoteLen),
					end:         end - uint(quoteLen),
					addEllipsis: true,
				})
			}
		}
	}

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

	return string(newSrc), nil
}