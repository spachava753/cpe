package codemap

import (
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	gitignore "github.com/sabhiram/go-gitignore"
	sitter "github.com/tree-sitter/go-tree-sitter"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// FileCodeMap represents a single file's code map output
type FileCodeMap struct {
	Path    string
	Content string
}

// GenerateOutput creates the code map output for each file using AST
func GenerateOutput(fsys fs.FS, maxLiteralLen int, ignorer *gitignore.GitIgnore) ([]FileCodeMap, error) {
	var filePaths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || ignorer.MatchesPath(path) {
			return nil
		}

		// Read file content
		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("error reading file %s: %w", path, err)
		}

		// Detect if file is text
		mime := mimetype.Detect(content)
		if !strings.HasPrefix(mime.String(), "text/") {
			return nil
		}

		filePaths = append(filePaths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking directory: %w", err)
	}

	sort.Strings(filePaths)

	var results []FileCodeMap
	for _, path := range filePaths {
		fileContent, err := generateFileOutput(fsys, path, maxLiteralLen)
		if err != nil {
			return nil, fmt.Errorf("error generating output for file %s: %w", path, err)
		}
		results = append(results, FileCodeMap{
			Path:    path,
			Content: fileContent,
		})
	}

	return results, nil
}

func generateFileOutput(fsys fs.FS, path string, maxLiteralLen int) (string, error) {
	src, err := fs.ReadFile(fsys, path)
	if err != nil {
		return "", err
	}

	var output string
	ext := filepath.Ext(path)

	switch ext {
	case ".go":
		output, err = generateGoFileOutput(src, maxLiteralLen)
	case ".java":
		output, err = generateJavaFileOutput(src, maxLiteralLen)
	case ".py":
		output, err = generatePythonFileOutput(src, maxLiteralLen)
	case ".js", ".jsx":
		output, err = generateJavaScriptFileOutput(src, maxLiteralLen)
	case ".ts", ".tsx":
		output, err = generateTypeScriptFileOutput(src, maxLiteralLen)
	default:
		output = string(src)
	}

	if err != nil {
		return "", fmt.Errorf("error generating output for file %s: %w", path, err)
	}

	return output, nil
}

func convertQueryError(queryType string, err *sitter.QueryError) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("error creating %s: %s (row: %d, column: %d, offset: %d, kind: %v)",
		queryType, err.Message, err.Row, err.Column, err.Offset, err.Kind)
}
