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

// buildTokenTree builds a tree of directories and files with their token counts
func buildTokenTree(fsys fs.FS, ignorer *gitignore.GitIgnore) (map[string]int, error) {
	// Initialize tiktoken
	loader := tiktokenloader.NewOfflineLoader()
	tiktoken.SetBpeLoader(loader)
	encoding, err := tiktoken.GetEncoding("o200k_base")
	if err != nil {
		return nil, fmt.Errorf("error initializing tiktoken: %w", err)
	}

	tt := make(map[string]int)

	// Walk the directory tree
	err = fs.WalkDir(fsys, ".", func(currentPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Check if the path should be ignored
		if ignorer.MatchesPath(currentPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			// Initialize directory with zero tokens
			tt[currentPath] = 0
		} else {
			// Read and count tokens for files
			content, err := os.ReadFile(currentPath)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", currentPath, err)
			}

			tokens := encoding.Encode(string(content), nil, nil)
			tokenCount := len(tokens)

			// Store the file's token count
			tt[currentPath] = tokenCount

			// Add token count to all parent directories
			dir := filepath.Dir(currentPath)
			for dir != "." {
				tt[dir] += tokenCount
				dir = filepath.Dir(dir)
			}
			// Add to root directory
			tt["."] += tokenCount
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return tt, nil
}

// PrintTokenTree prints a formatted representation of the token tree
func PrintTokenTree(fsys fs.FS, ignorer *gitignore.GitIgnore) error {
	tree, err := buildTokenTree(fsys, ignorer)
	if err != nil {
		return err
	}

	// Walk the directory tree again just for printing in order
	return fs.WalkDir(fsys, ".", func(currentPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if ignorer.MatchesPath(currentPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Calculate the indentation based on directory depth
		depth := len(strings.Split(currentPath, string(os.PathSeparator))) - 1
		indent := strings.Repeat("  ", depth)

		if d.IsDir() {
			fmt.Printf("%s📁 %s/ (%d tokens)\n", indent, d.Name(), tree[currentPath])
		} else {
			fmt.Printf("%s📄 %s (%d tokens)\n", indent, d.Name(), tree[currentPath])
		}

		return nil
	})
}
