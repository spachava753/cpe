package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goyek/goyek/v2"
)

// Lint runs golangci-lint and custom linters on the codebase
var Lint = goyek.Define(goyek.Task{
	Name:  "lint",
	Usage: "Run golangci-lint and custom linters. Use -lint-fix to auto-fix, -lint-verbose for details",
	Action: func(a *goyek.A) {
		// Run golangci-lint
		args := []string{"tool", "golangci-lint", "run"}

		if GetLintFix() {
			args = append(args, "--fix")
		}

		if GetLintVerbose() {
			args = append(args, "-v")
		}

		args = append(args, "./...")

		cmd := exec.Command("go", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				a.Errorf("Linting issues found (exit code %d)", exitErr.ExitCode())
			} else {
				a.Fatalf("Failed to run golangci-lint: %v", err)
			}
		}

		// Run custom cmd package linter
		if issues := lintCmdPackage(); len(issues) > 0 {
			for _, issue := range issues {
				fmt.Println(issue)
			}
			a.Errorf("found %d function(s) in cmd package that should be moved to ./internal", len(issues))
		}
	},
})

// lintCmdPackage ensures cmd package only has cobra setup (no business logic functions).
// Returns a list of issues found.
func lintCmdPackage() []string {
	fset := token.NewFileSet()

	entries, err := os.ReadDir("cmd")
	if err != nil {
		return []string{fmt.Sprintf("failed to read cmd directory: %v", err)}
	}

	var issues []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join("cmd", entry.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			issues = append(issues, fmt.Sprintf("failed to parse %s: %v", path, err))
			continue
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			// Allow init functions
			if fn.Name.Name == "init" && fn.Recv == nil {
				continue
			}

			// Allow Execute function (cobra entry point)
			if fn.Name.Name == "Execute" && fn.Recv == nil {
				continue
			}

			pos := fset.Position(fn.Pos())
			if fn.Recv != nil {
				issues = append(issues, fmt.Sprintf("%s:%d: method %q: business logic should be in ./internal packages, not ./cmd",
					pos.Filename, pos.Line, fn.Name.Name))
			} else {
				issues = append(issues, fmt.Sprintf("%s:%d: function %q: business logic should be in ./internal packages, not ./cmd",
					pos.Filename, pos.Line, fn.Name.Name))
			}
		}
	}

	return issues
}
