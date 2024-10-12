package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

type IgnoreRules struct {
	patterns []glob.Glob
}

func NewIgnoreRules() *IgnoreRules {
	return &IgnoreRules{
		patterns: []glob.Glob{},
	}
}

func (ir *IgnoreRules) LoadIgnoreFile(startDir string) error {
	ignoreFile := findIgnoreFile(startDir)
	if ignoreFile == "" {
		return nil // No .cpeignore file found, which is okay
	}

	file, err := os.Open(ignoreFile)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		pattern := strings.TrimSpace(scanner.Text())
		if pattern != "" && !strings.HasPrefix(pattern, "#") {
			g, err := glob.Compile(pattern)
			if err != nil {
				return err
			}
			ir.patterns = append(ir.patterns, g)
		}
	}

	return scanner.Err()
}

func (ir *IgnoreRules) ShouldIgnore(path string) bool {
	for _, pattern := range ir.patterns {
		if pattern.Match(path) {
			return true
		}
	}
	return false
}

func findIgnoreFile(startDir string) string {
	dir := startDir
	for {
		ignoreFile := filepath.Join(dir, ".cpeignore")
		if _, err := os.Stat(ignoreFile); err == nil {
			return ignoreFile
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
