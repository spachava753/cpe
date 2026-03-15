package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

const (
	commandsImportRuleReason      = "cmd may only import internal/commands and internal/version from this module"
	configImportRuleReason        = "internal/config must not depend on internal/mcp; share schema via a neutral package instead"
	commandsAgentImportRuleReason = "internal/commands should depend on internal/agent only from root.go and mcp.go runtime orchestration files"
	agentOwnershipRuleReason      = "internal/agent must not own input or prompt concerns; use internal/input and internal/prompt instead"
)

// lintCmdPackageAt reports cmd-package declarations that should move to ./internal.
// The only allowed package-level functions are init and Execute.
func lintCmdPackageAt(root string) []string {
	fset := token.NewFileSet()
	cmdDir := filepath.Join(root, "cmd")

	var issues []string
	err := walkGoFiles(cmdDir, func(path string) {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			issues = append(issues, fmt.Sprintf("failed to parse %s: %v", path, err))
			return
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv == nil && (fn.Name.Name == "init" || fn.Name.Name == "Execute") {
				continue
			}

			pos := fset.Position(fn.Pos())
			if fn.Recv != nil {
				issues = append(issues, fmt.Sprintf("%s:%d: method %q: business logic should be in ./internal packages, not ./cmd", pos.Filename, pos.Line, fn.Name.Name))
			} else {
				issues = append(issues, fmt.Sprintf("%s:%d: function %q: business logic should be in ./internal packages, not ./cmd", pos.Filename, pos.Line, fn.Name.Name))
			}
		}
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []string{fmt.Sprintf("failed to walk %s: %v", cmdDir, err)}
	}

	return issues
}

type importBoundaryRule struct {
	packageDir            string
	allowedInternalImport map[string]bool
	disallowedImports     map[string]string
}

// lintImportBoundariesAt enforces package import boundaries that keep cmd thin
// and keep internal/commands free of Cobra/pflag coupling.
func lintImportBoundariesAt(root string) []string {
	modulePath, err := modulePathFromGoMod(root)
	if err != nil {
		return []string{err.Error()}
	}

	rules := []importBoundaryRule{
		{
			packageDir: filepath.Join("cmd"),
			allowedInternalImport: map[string]bool{
				modulePath + "/internal/commands": true,
				modulePath + "/internal/version":  true,
			},
		},
		{
			packageDir: filepath.Join("internal", "commands"),
			disallowedImports: map[string]string{
				"github.com/spf13/cobra": "Cobra belongs in cmd; internal/commands must stay framework-agnostic",
				"github.com/spf13/pflag": "pflag belongs in cmd; internal/commands must stay framework-agnostic",
			},
		},
		{
			packageDir: filepath.Join("internal", "config"),
			disallowedImports: map[string]string{
				modulePath + "/internal/mcp": configImportRuleReason,
			},
		},
	}

	var issues []string
	for _, rule := range rules {
		issues = append(issues, lintImportBoundaryRule(root, modulePath, rule)...)
	}
	issues = append(issues, lintCommandsAgentImportsAt(root, modulePath)...)
	issues = append(issues, lintAgentOwnershipAt(root, modulePath)...)
	return issues
}

func lintImportBoundaryRule(root, modulePath string, rule importBoundaryRule) []string {
	fset := token.NewFileSet()
	dir := filepath.Join(root, rule.packageDir)

	var issues []string
	err := walkGoFiles(dir, func(path string) {
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			issues = append(issues, fmt.Sprintf("failed to parse %s: %v", path, err))
			return
		}

		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			pos := fset.Position(imp.Pos())

			if reason, forbidden := rule.disallowedImports[importPath]; forbidden {
				issues = append(issues, fmt.Sprintf("%s:%d: import %q: %s", pos.Filename, pos.Line, importPath, reason))
				continue
			}

			if !strings.HasPrefix(importPath, modulePath+"/") {
				continue
			}
			if len(rule.allowedInternalImport) == 0 {
				continue
			}
			if !rule.allowedInternalImport[importPath] {
				issues = append(issues, fmt.Sprintf("%s:%d: import %q: %s", pos.Filename, pos.Line, importPath, commandsImportRuleReason))
			}
		}
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []string{fmt.Sprintf("failed to walk %s: %v", dir, err)}
	}

	return issues
}

func lintCommandsAgentImportsAt(root, modulePath string) []string {
	allowedFiles := map[string]bool{
		filepath.Join(root, "internal", "commands", "mcp.go"):  true,
		filepath.Join(root, "internal", "commands", "root.go"): true,
	}
	return lintSpecificImportUsage(root, filepath.Join("internal", "commands"), modulePath+"/internal/agent", commandsAgentImportRuleReason, func(path string) bool {
		return allowedFiles[filepath.Clean(path)]
	})
}

func lintAgentOwnershipAt(root, modulePath string) []string {
	var issues []string
	issues = append(issues, lintSpecificImportUsage(root, filepath.Join("internal", "agent"), modulePath+"/internal/input", agentOwnershipRuleReason, nil)...)
	issues = append(issues, lintSpecificImportUsage(root, filepath.Join("internal", "agent"), modulePath+"/internal/prompt", agentOwnershipRuleReason, nil)...)
	return issues
}

func lintSpecificImportUsage(root, packageDir, disallowedImportPath, reason string, allowFile func(path string) bool) []string {
	fset := token.NewFileSet()
	dir := filepath.Join(root, packageDir)

	var issues []string
	err := walkGoFiles(dir, func(path string) {
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			issues = append(issues, fmt.Sprintf("failed to parse %s: %v", path, err))
			return
		}

		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			if importPath != disallowedImportPath {
				continue
			}
			if allowFile != nil && allowFile(path) {
				continue
			}
			pos := fset.Position(imp.Pos())
			issues = append(issues, fmt.Sprintf("%s:%d: import %q: %s", pos.Filename, pos.Line, importPath, reason))
		}
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []string{fmt.Sprintf("failed to walk %s: %v", dir, err)}
	}

	return issues
}

func modulePathFromGoMod(root string) (string, error) {
	goModPath := filepath.Join(root, "go.mod")
	goModBytes, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", goModPath, err)
	}

	parsed, err := modfile.Parse(goModPath, goModBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s: %w", goModPath, err)
	}
	if parsed.Module == nil || parsed.Module.Mod.Path == "" {
		return "", fmt.Errorf("module path not found in %s", goModPath)
	}
	return parsed.Module.Mod.Path, nil
}

func walkGoFiles(dir string, visit func(path string)) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") || strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		visit(path)
		return nil
	})
}
