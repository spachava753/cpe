package codemode

import (
	"bytes"
	"errors"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"
)

const (
	// mcpSDKImport is the import path for the MCP SDK package
	mcpSDKImport = "github.com/modelcontextprotocol/go-sdk/mcp"
)

// correctFileImports runs goimports in a separate child process so workspace-
// specific env overrides stay isolated to that process.
func correctFileImports(dir, filename string) (string, error) {
	filePath := filepath.Join(dir, filename)
	orig, err := os.Open(filePath)
	if err != nil {
		return "", err
	}

	origImports, err := extractImports(orig)
	if err != nil {
		return "", recoverableSyntaxError(err)
	}
	offset, err := orig.Seek(0, io.SeekStart)
	if err != nil {
		return "", err
	}
	if offset != 0 {
		panic("expected offset to be 0")
	}

	tempFile, err := os.CreateTemp(dir, "cpe-goimports-*.go")
	if err != nil {
		return "", err
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath)
	}()

	if err := ensureMCPImport(orig, tempFile); err != nil {
		return "", recoverableSyntaxError(err)
	}

	// imports will read the file contents on its own
	newFile, err := imports.Process(orig.Name(), nil, nil)
	if err != nil {
		return "", recoverableSyntaxError(err)
	}

	formattedImports, err := extractImports(bytes.NewReader(newFile))
	if err != nil {
		return "", recoverableSyntaxError(err)
	}

	if _, err := tempFile.Write(newFile); err != nil {
		return "", nil
	}

	return diff(origImports, formattedImports), nil
}

func recoverableSyntaxError(err error) error {
	if err == nil {
		return nil
	}
	if el, ok := errors.AsType[scanner.ErrorList](err); ok {
		return RecoverableError{Err: el.Err()}
	}
	return err
}

func diff(a, b []string) string {
	aSet := make(map[string]struct{})
	for i := range a {
		aSet[a[i]] = struct{}{}
	}

	bSet := make(map[string]struct{})
	for i := range b {
		bSet[b[i]] = struct{}{}
	}

	// get the intersection
	intersection := make(map[string]struct{})
	for i := range bSet {
		if _, ok := aSet[i]; ok {
			intersection[i] = struct{}{}
		}
	}

	var additions, deletions []string
	var sb strings.Builder

	// a - intersection = imports removed
	for i := range aSet {
		if _, ok := intersection[i]; !ok {
			deletions = append(deletions, i)
		}
	}
	// b - intersection = imports added
	for i := range bSet {
		if _, ok := intersection[i]; !ok {
			additions = append(additions, i)
		}
	}

	if len(additions) > 0 || len(deletions) > 0 {
		sb.WriteString("imports adjusted:\n")
		for _, d := range deletions {
			sb.WriteString(`- `)
			sb.WriteString(d)
		}
		for _, ad := range additions {
			sb.WriteString(`+ `)
			sb.WriteString(ad)
		}
	}

	return sb.String()
}

// ensureMCPImport adds the MCP SDK import if not already present.
// This prevents goimports from resolving mcp to the wrong package.
// The import will be removed by goimports if not actually used.
func ensureMCPImport(src io.Reader, dst io.Writer) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return err
	}

	astutil.AddImport(fset, f, mcpSDKImport)

	if err := printer.Fprint(dst, fset, f); err != nil {
		return err
	}

	return nil
}

// extractImports parses Go source and returns a set of import paths.
func extractImports(src io.Reader) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	imps := make(map[string]struct{})
	for _, imp := range f.Imports {
		// Remove quotes from import path
		path := strings.Trim(imp.Path.Value, "`\"")
		imps[path] = struct{}{}
	}
	return slices.Collect(maps.Keys(imps)), nil
}
