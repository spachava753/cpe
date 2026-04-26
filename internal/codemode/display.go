package codemode

import (
	"fmt"
	"strings"
)

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
