package codemap

import (
	"fmt"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

func generatePythonFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	pythonLang := sitter.NewLanguage(python.Language())
	err := parser.SetLanguage(pythonLang)
	if err != nil {
		return "", err
	}

	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Query for function definitions and their doc comments
	funcQuery, queryErr := sitter.NewQuery(pythonLang, `
		(function_definition
		  name: (identifier) @func.name
		  body: (block
			(expression_statement
			  (string) @func.docstring)?
			.
			_*) @func.body)
	`)
	if queryErr != nil {
		return "", convertQueryError("function query", queryErr)
	}
	defer funcQuery.Close()

	// Query for string literals
	stringLiteralQuery, queryErr := sitter.NewQuery(pythonLang, `
		(string) @string
	`)
	if queryErr != nil {
		return "", convertQueryError("string literal query", queryErr)
	}
	defer stringLiteralQuery.Close()

	// Execute queries
	funcCursor := sitter.NewQueryCursor()
	defer funcCursor.Close()
	funcMatches := funcCursor.Matches(funcQuery, root, src)

	// Collect positions to cut
	type transformation struct {
		cutStart, cutEnd uint
		addEllipsis      bool
		prependText      string
	}
	cutRanges := make([]transformation, 0)

	// Collect function body ranges and preserve docstrings
	for match := funcMatches.Next(); match != nil; match = funcMatches.Next() {
		var bodyStart, bodyEnd uint
		var prependText string

		for _, capture := range match.Captures {
			switch capture.Index {
			case 1:
				bodyStart = capture.Node.EndByte()
				// find out the indent size
				indents := capture.Node.StartPosition().Column
				prependText = "\n" + strings.Repeat(" ", int(indents))
			case 2:
				bodyStart = capture.Node.StartByte()
				bodyEnd = capture.Node.EndByte()
			}
		}

		if bodyStart > bodyEnd {
			panic("unexpected condition")
		}

		if bodyStart != bodyEnd {
			cutRanges = append(cutRanges, transformation{
				cutStart:    bodyStart,
				cutEnd:      bodyEnd,
				prependText: prependText,
			})
		}
	}

	// Sort cutRanges by cutStart position
	sort.Slice(cutRanges, func(i, j int) bool {
		return cutRanges[i].cutStart < cutRanges[j].cutStart
	})

	// Remove subset ranges and check for overlaps
	var filteredRanges []transformation
	for i, r := range cutRanges {
		isSubset := false
		for j, other := range cutRanges {
			if i != j {
				if r.cutStart >= other.cutStart && r.cutEnd <= other.cutEnd {
					isSubset = true
					break
				}
				if (r.cutStart < other.cutStart && r.cutEnd > other.cutStart && r.cutEnd < other.cutEnd) ||
					(r.cutStart > other.cutStart && r.cutStart < other.cutEnd && r.cutEnd > other.cutEnd) {
					return "", fmt.Errorf("overlapping cut ranges detected: %v and %v", r, other)
				}
			}
		}
		if !isSubset {
			filteredRanges = append(filteredRanges, r)
		}
	}

	// Create new source with truncated string literals and without function, class, and method bodies
	var newSrc []byte
	lastEnd := uint(0)
	for _, r := range filteredRanges {
		newSrc = append(newSrc, src[lastEnd:r.cutStart]...)
		newSrc = append(newSrc, []byte(r.prependText)...)
		if r.addEllipsis {
			newSrc = append(newSrc, []byte("...")...)
		} else {
			newSrc = append(newSrc, []byte("pass")...)
		}

		lastEnd = r.cutEnd
	}
	newSrc = append(newSrc, src[lastEnd:]...)

	return strings.TrimSpace(string(newSrc)), nil
}
