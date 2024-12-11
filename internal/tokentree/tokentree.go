package tokentree

import (
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/tiktokenloader"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PrintTokenTree prints a tree of directories and files with their token counts
func PrintTokenTree(fsys fs.FS, ignorer *gitignore.GitIgnore) error {
	// Initialize tiktoken
	loader := tiktokenloader.NewOfflineLoader()
	tiktoken.SetBpeLoader(loader)
	encoding, err := tiktoken.GetEncoding("o200k_base")
	if err != nil {
		return fmt.Errorf("error initializing tiktoken: %w", err)
	}

	// Walk the directory tree
	return fs.WalkDir(fsys, ".", func(currentPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Calculate the indentation based on directory depth
		depth := len(strings.Split(currentPath, string(os.PathSeparator))) - 1
		indent := strings.Repeat("  ", depth)

		// Check if the path should be ignored
		if ignorer.MatchesPath(currentPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			// Print directory name
			fmt.Printf("%s📁 %s/\n", indent, d.Name())
		} else {
			// Read and count tokens for files
			content, err := os.ReadFile(currentPath)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", currentPath, err)
			}

			tokens := encoding.Encode(string(content), nil, nil)
			tokenCount := len(tokens)

			// Print file name and token count
			fmt.Printf("%s📄 %s (%d tokens)\n", indent, d.Name(), tokenCount)
		}

		return nil
	})
}
