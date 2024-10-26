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

func (ir *IgnoreRules) LoadIgnoreFiles(startDir string) error {
	ignoreFiles := findIgnoreFiles(startDir)
	for _, ignoreFile := range ignoreFiles {
		if err := ir.loadIgnoreFile(ignoreFile); err != nil {
			return err
		}
	}
	return nil
}

func (ir *IgnoreRules) loadIgnoreFile(ignoreFile string) error {
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

func findIgnoreFiles(startDir string) []string {
	var ignoreFiles []string
	dir, err := filepath.Abs(startDir)
	if err != nil {
		panic("Could not find absolute start dir: " + startDir)
	}
	for {
		ignoreFile := filepath.Join(dir, ".cpeignore")
		if _, err := os.Stat(ignoreFile); err == nil {
			ignoreFiles = append(ignoreFiles, ignoreFile)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ignoreFiles
}
