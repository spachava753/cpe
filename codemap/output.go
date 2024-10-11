package codemap

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

var supportedLanguages = map[string]bool{
	".go":   true,
	".java": true,
	".c":    true,
	".h":    true,
	".py":   true,
	".js":   true,
	".ts":   true,
	".sql":  true,
	".yaml": true,
	".yml":  true,
	".json": true,
	".xml":  true,
	".mod":  true,
}

// GenerateOutput creates the XML-like output for the code map using AST
func GenerateOutput(fsys fs.FS, maxLiteralLen int) (string, error) {
	var sb strings.Builder
	sb.WriteString("<code_map>\n")

	var filePaths []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && isSourceCode(path) {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error walking directory: %w", err)
	}

	sort.Strings(filePaths)

	for _, path := range filePaths {
		fileContent, err := generateFileOutput(fsys, path, maxLiteralLen)
		if err != nil {
			return "", fmt.Errorf("error generating output for file %s: %w", path, err)
		}
		sb.WriteString(fileContent)
	}

	sb.WriteString("</code_map>\n")
	return sb.String(), nil
}

func isSourceCode(path string) bool {
	ext := filepath.Ext(path)
	return supportedLanguages[ext]
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
	default:
		output = string(src)
	}

	if err != nil {
		return "", fmt.Errorf("error generating output for file %s: %w", path, err)
	}

	return fmt.Sprintf("<file>\n<path>%s</path>\n<file_map>\n%s\n</file_map>\n</file>\n", path, output), nil
}

func convertQueryError(queryType string, err *sitter.QueryError) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("error creating %s: %s (row: %d, column: %d, offset: %d, kind: %v)",
		queryType, err.Message, err.Row, err.Column, err.Offset, err.Kind)
}
