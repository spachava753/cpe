package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

type Patterns struct {
	patterns []glob.Glob
}

var defaultPatterns = []string{
	".git/**",
}

func NewIgnoreRules() *Patterns {
	ir := &Patterns{
		patterns: []glob.Glob{},
	}

	// Add default patterns
	for _, pattern := range defaultPatterns {
		if g, err := glob.Compile(pattern); err == nil {
			ir.patterns = append(ir.patterns, g)
		}
	}

	return ir
}

func (ir *Patterns) LoadIgnoreFiles(startDir string) error {
	ignoreFiles := findIgnoreFiles(startDir)
	for _, ignoreFile := range ignoreFiles {
		if err := ir.loadIgnoreFile(ignoreFile); err != nil {
			return err
		}
	}
	return nil
}

func (ir *Patterns) loadIgnoreFile(ignoreFile string) error {
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

func (ir *Patterns) ShouldIgnore(path string) bool {
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
