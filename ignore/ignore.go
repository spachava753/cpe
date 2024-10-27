package ignore

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

type Patterns struct {
	ignorer *gitignore.GitIgnore
}

var defaultPatterns = []string{
	".git/**",
}

func NewIgnoreRules() *Patterns {
	return &Patterns{}
}

func (ir *Patterns) LoadIgnoreFiles(startDir string) error {
	ignoreFiles := findIgnoreFiles(startDir)

	var allPatterns []string
	// Add default patterns first
	allPatterns = append(allPatterns, defaultPatterns...)

	// Read patterns from all ignore files
	for _, ignoreFile := range ignoreFiles {
		content, err := os.ReadFile(ignoreFile)
		if err != nil {
			return err
		}
		// Split content into lines and add non-empty, non-comment lines
		lines := strings.Split(string(content), "\n")
		allPatterns = append(allPatterns, lines...)
	}

	// Create a new GitIgnore instance with all patterns
	ir.ignorer = gitignore.CompileIgnoreLines(allPatterns...)
	return nil
}

func (ir *Patterns) ShouldIgnore(path string) bool {
	if ir.ignorer == nil {
		return false
	}
	return ir.ignorer.MatchesPath(path)
}

// findIgnoreFiles finds all .cpeignore files in the directory hierarchy
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
