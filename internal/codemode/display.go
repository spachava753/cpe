package codemode

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatDisplayCodeWithLineNumbers normalizes source text and prefixes each line
// with a 1-based line number so compiler diagnostics can be mapped back to the
// generated run.go shown in the CLI.
func FormatDisplayCodeWithLineNumbers(code string) string {
	normalized := strings.ReplaceAll(code, "\r\n", "\n")
	if normalized == "" {
		return ""
	}
	normalized = strings.TrimSuffix(normalized, "\n")

	lines := strings.Split(normalized, "\n")
	width := len(strconv.Itoa(len(lines)))
	for i, line := range lines {
		lines[i] = fmt.Sprintf("%*d  %s", width, i+1, line)
	}

	return strings.Join(lines, "\n")
}

// MarkdownFencedBlock returns content wrapped in a markdown code fence that is
// guaranteed not to be prematurely closed by backtick runs contained in content.
func MarkdownFencedBlock(language, content string) string {
	fenceLen := maxBacktickRun(content) + 1
	if fenceLen < 3 {
		fenceLen = 3
	}
	fence := strings.Repeat("`", fenceLen)
	if language != "" {
		return fmt.Sprintf("%s%s\n%s\n%s", fence, language, content, fence)
	}
	return fmt.Sprintf("%s\n%s\n%s", fence, content, fence)
}

func maxBacktickRun(s string) int {
	maxRun := 0
	currentRun := 0
	for _, r := range s {
		if r == '`' {
			currentRun++
			if currentRun > maxRun {
				maxRun = currentRun
			}
			continue
		}
		currentRun = 0
	}
	return maxRun
}
