package codemap

import (
	"context"
	"fmt" // Added for fmt.Errorf
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

var (
	goOnce     sync.Once
	goParser   *sitter.Parser
	goLanguage *sitter.Language

	javaOnce     sync.Once
	javaParser   *sitter.Parser
	javaLanguage *sitter.Language

	jsOnce     sync.Once
	jsParser   *sitter.Parser
	jsLanguage *sitter.Language

	pyOnce     sync.Once
	pyParser   *sitter.Parser
	pyLanguage *sitter.Language

	tsOnce      sync.Once
	tsParser    *sitter.Parser
	tsLanguage  *sitter.Language // For .ts
	tsxLanguage *sitter.Language // For .tsx
)

func initGoParserForSyntaxCheck() {
	goOnce.Do(func() {
		goLanguage = sitter.NewLanguage(golang.Language())
		goParser = sitter.NewParser()
		goParser.SetLanguage(goLanguage)
	})
}

func initJavaParserForSyntaxCheck() {
	javaOnce.Do(func() {
		javaLanguage = sitter.NewLanguage(java.Language())
		javaParser = sitter.NewParser()
		javaParser.SetLanguage(javaLanguage)
	})
}

func initJsParserForSyntaxCheck() {
	jsOnce.Do(func() {
		jsLanguage = sitter.NewLanguage(javascript.Language())
		jsParser = sitter.NewParser()
		jsParser.SetLanguage(jsLanguage)
	})
}

func initPyParserForSyntaxCheck() {
	pyOnce.Do(func() {
		pyLanguage = sitter.NewLanguage(python.Language())
		pyParser = sitter.NewParser()
		pyParser.SetLanguage(pyLanguage)
	})
}

func initTsParserForSyntaxCheck() {
	tsOnce.Do(func() {
		tsLanguage = sitter.NewLanguage(typescript.LanguageTypescript()) // Corrected
		tsxLanguage = sitter.NewLanguage(typescript.LanguageTSX())       // Corrected
		tsParser = sitter.NewParser()
	})
}

func CheckSyntax(ctx context.Context, filePath string, content []byte) (hasError bool, parserFound bool, err error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	var currentParser *sitter.Parser
	var langToSet *sitter.Language

	switch ext {
	case ".go":
		initGoParserForSyntaxCheck()
		currentParser = goParser
	case ".java":
		initJavaParserForSyntaxCheck()
		currentParser = javaParser
	case ".js", ".jsx":
		initJsParserForSyntaxCheck()
		currentParser = jsParser
	case ".py":
		initPyParserForSyntaxCheck()
		currentParser = pyParser
	case ".ts":
		initTsParserForSyntaxCheck()
		currentParser = tsParser
		langToSet = tsLanguage
	case ".tsx":
		initTsParserForSyntaxCheck()
		currentParser = tsParser
		langToSet = tsxLanguage
	default:
		return false, false, nil // No parser for this extension
	}

	if currentParser == nil {
		return false, false, nil // Should not happen if ext matched
	}

	if langToSet != nil { // Set specific language for TS/TSX
		currentParser.SetLanguage(langToSet)
	}

	// Channel to receive the parsed tree or nil if parsing fails internally
	// Buffer size 1 to prevent goroutine leak if select chooses ctx.Done() first
	// and the goroutine tries to send.
	resultChan := make(chan *sitter.Tree, 1)

	go func() {
		// oldTree is nil as we are parsing fresh content or don't have a prior tree for this check
		parsedTree := currentParser.Parse(content, nil)
		resultChan <- parsedTree
	}()

	var tree *sitter.Tree
	select {
	case <-ctx.Done():
		// Context was cancelled before parsing completed
		return false, true, fmt.Errorf("syntax check for %s cancelled: %w", filePath, ctx.Err())
	case receivedTree := <-resultChan:
		tree = receivedTree
	}

	// If tree is nil, it might mean empty input (which Parse handles by returning nil tree)
	// or some other critical internal parser failure not related to syntax errors in content.
	if tree == nil {
		// For empty content, tree-sitter's Parse returns nil, which is not a syntax error.
		if len(content) == 0 {
			return false, true, nil
		}
		// For non-empty content, a nil tree indicates a more fundamental parsing issue.
		return false, true, fmt.Errorf("parsing failed critically for %s (parser returned nil tree for non-empty content)", filePath)
	}
	defer tree.Close()

	if tree.RootNode() == nil {
		// This case should be rare if tree itself is not nil, but good for robustness.
		// An empty file might result in a nil root node but isn't necessarily a syntax error.
		if len(content) > 0 {
			return true, true, fmt.Errorf("parsing yielded no root node for non-empty file %s", filePath)
		}
		return false, true, nil
	}

	return tree.RootNode().HasError(), true, nil
}
