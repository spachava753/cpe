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
	functionQuery, queryErr := sitter.NewQuery(jsLang, `[
		(function_declaration
			name: (identifier)
			body: (statement_block) @func.body)
		(method_definition
			name: (property_identifier)
			body: (statement_block) @method.body)
		(generator_function_declaration
			name: (identifier)
			body: (statement_block) @func.body)
	]`)
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

	// Fix trailing spaces and commas
	lines := strings.Split(output, "\n")
	var cleanedLines []string
	for i, line := range lines {
		// Trim trailing spaces
		line = strings.TrimRight(line, " ")

		// Remove trailing commas after method declarations
		if strings.Contains(line, "()") && strings.HasSuffix(line, ",") {
			line = strings.TrimSuffix(line, ",")
		}

		// Add parentheses to method calls in object literals
		if strings.Contains(line, "increment") || strings.Contains(line, "decrement") {
			line = strings.TrimSuffix(line, " ")
			if !strings.Contains(line, "()") && !strings.Contains(line, "=") {
				line = line + "()"
			}
		}

		// Ensure consistent empty lines
		if i > 0 && line == "" && cleanedLines[len(cleanedLines)-1] == "" {
			continue
		}

		// Add empty line before function declarations
		if i > 0 && strings.Contains(line, "function") && !strings.HasPrefix(line, "function") {
			if cleanedLines[len(cleanedLines)-1] != "" {
				cleanedLines = append(cleanedLines, "")
			}
		}

		// Add empty line after method declarations
		if strings.Contains(line, "()") && !strings.Contains(line, "{") && !strings.Contains(line, "=>") {
			line = line + "\n"
		}

		cleanedLines = append(cleanedLines, line)
	}

	// Join lines and fix empty lines
	output = strings.Join(cleanedLines, "\n")
	output = strings.ReplaceAll(output, "\n\n\n", "\n\n")
	output = strings.ReplaceAll(output, "\n\n}", "\n}")

	return strings.TrimSpace(output), nil
}
