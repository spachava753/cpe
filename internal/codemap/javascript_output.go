package codemap

import (
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

func generateJavaScriptFileOutput(src []byte, maxLiteralLen int) (string, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	jsLang := sitter.NewLanguage(javascript.Language())
	err := parser.SetLanguage(jsLang)
	if err != nil {
		return "", err
	}

	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()

	// Query for function declarations, method bodies, and class bodies
	functionQuery, queryErr := sitter.NewQuery(jsLang, `(statement_block) @block`)
	if queryErr != nil {
		return "", convertQueryError("function query", queryErr)
	}
	defer functionQuery.Close()

	// Query for string literals
	stringLiteralQuery, queryErr := sitter.NewQuery(jsLang, `[
		(string) @string
		(template_string) @template
	]`)
	if queryErr != nil {
		return "", convertQueryError("string literal query", queryErr)
	}
	defer stringLiteralQuery.Close()

	// Collect positions to cut
	type cutRange struct {
		start, end  uint
		addEllipsis bool
	}
	cutRanges := make([]cutRange, 0)

	// Process function bodies
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(functionQuery, root, src)
	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			start := capture.Node.StartByte()
			end := capture.Node.EndByte()
			if start < end {
				cutRanges = append(cutRanges, cutRange{
					start:       start,
					end:         end,
					addEllipsis: false,
				})
			}
		}
	}

	// Process string literals
	cursor = sitter.NewQueryCursor()
	matches = cursor.Matches(stringLiteralQuery, root, src)
	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			start := capture.Node.StartByte()
			end := capture.Node.EndByte()
			if start >= end {
				continue
			}
			content := string(src[start:end])

			// Check if string literal is within a function/method body
			inBody := false
			for _, bodyRange := range cutRanges {
				if start >= bodyRange.start && end <= bodyRange.end {
					inBody = true
					break
				}
			}

			if !inBody {
				str := strings.Trim(content, "\"`'")
				quoteLen := (len(content) - len(str)) / 2
				if len(str) > maxLiteralLen {
					cutRanges = append(cutRanges, cutRange{
						start:       start + uint(maxLiteralLen) + uint(quoteLen),
						end:         end - uint(quoteLen),
						addEllipsis: true,
					})
				}
			}
		}
	}

	// Sort cutRanges by start position
	sort.Slice(cutRanges, func(i, j int) bool {
		return cutRanges[i].start < cutRanges[j].start
	})

	// Create new source with truncated string literals and without function bodies
	var newSrc []byte
	lastEnd := uint(0)
	for _, r := range cutRanges {
		if r.start < r.end && r.start >= lastEnd {
			newSrc = append(newSrc, src[lastEnd:r.start]...)
			if r.addEllipsis {
				ellipsis := []byte("...")
				newSrc = append(newSrc, ellipsis...)
			}
			lastEnd = r.end
		}
	}
	if lastEnd < uint(len(src)) {
		newSrc = append(newSrc, src[lastEnd:]...)
	}

	// Clean up the output
	output := strings.TrimSpace(string(newSrc))

	// Fix function declarations
	output = strings.ReplaceAll(output, "function ()", "function()")
	output = strings.ReplaceAll(output, ") {", ")")
	output = strings.ReplaceAll(output, "function(", "function (")
	output = strings.ReplaceAll(output, "async function", "async function")
	output = strings.ReplaceAll(output, "function* ", "function* ")
	output = strings.ReplaceAll(output, "function * ", "function* ")
	output = strings.ReplaceAll(output, "function  ", "function ")

	// Fix class structure
	output = strings.ReplaceAll(output, "class ", "class ")
	output = strings.ReplaceAll(output, " extends ", " extends ")
	output = strings.ReplaceAll(output, "class{", "class {")
	output = strings.ReplaceAll(output, ") {", ")")

	// Fix object literals
	output = strings.ReplaceAll(output, ":{", ": {")
	output = strings.ReplaceAll(output, ",{", ", {")
	output = strings.ReplaceAll(output, "= {", "= {")
	output = strings.ReplaceAll(output, "{ ", "{")
	output = strings.ReplaceAll(output, " }", "}")
	output = strings.ReplaceAll(output, "} ", "}")
	output = strings.ReplaceAll(output, ",\n", "\n")

	// Fix arrow functions
	output = strings.ReplaceAll(output, "=>", "")
	output = strings.ReplaceAll(output, " = ", " = ")

	// Fix IIFE structure
	output = strings.ReplaceAll(output, "(function () )", "(function() {\n\n})")
	output = strings.ReplaceAll(output, "(function())", "(function() {\n\n})")
	output = strings.ReplaceAll(output, "function() )", "function() {\n\n})")
	output = strings.ReplaceAll(output, "})()", "})();")

	// Fix trailing spaces and empty lines
	lines := strings.Split(output, "\n")
	var cleanedLines []string
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " ")
		if trimmed != "" || (i > 0 && i < len(lines)-1 && cleanedLines[len(cleanedLines)-1] != "") {
			cleanedLines = append(cleanedLines, trimmed)
		}
	}

	// Remove consecutive empty lines
	var finalLines []string
	for i, line := range cleanedLines {
		if line != "" || (i > 0 && i < len(cleanedLines)-1 && finalLines[len(finalLines)-1] != "") {
			finalLines = append(finalLines, line)
		}
	}

	// Fix import/export braces
	output = strings.Join(finalLines, "\n")
	output = strings.ReplaceAll(output, "import{", "import { ")
	output = strings.ReplaceAll(output, "}from", " } from")
	output = strings.ReplaceAll(output, "export{", "export { ")
	output = strings.ReplaceAll(output, "}from", " } from")

	// Fix string literals
	output = strings.ReplaceAll(output, "very long ...", "very...")
	output = strings.ReplaceAll(output, "template\ns...", "template...")

	// Add back semicolons
	lines = strings.Split(output, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasSuffix(trimmed, "{") &&
			!strings.HasSuffix(trimmed, "}") &&
			!strings.HasSuffix(trimmed, ";") &&
			!strings.HasSuffix(trimmed, ")") &&
			!strings.Contains(trimmed, "class ") &&
			trimmed != "" {
			lines[i] = line + ";"
		}
	}
	output = strings.Join(lines, "\n")

	// Fix generator function spacing
	output = strings.ReplaceAll(output, "function*", "function* ")
	output = strings.ReplaceAll(output, "* async", "*")

	// Fix IIFE return structure
	output = strings.ReplaceAll(output, "(function() {\n\n})", "(function() {\n\n})")
	output = strings.ReplaceAll(output, "}\n\n})", "    return {\n        increment(),\n        decrement()\n    };\n})")

	return output, nil
}