package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

type exportKey struct {
	pkgPath string
	name    string
}

type unnecessaryExport struct {
	key       exportKey
	kind      string
	newName   string
	position  token.Position
	locations map[string]sourceEdit
	fixable   bool
	reason    string
}

type sourceEdit struct {
	offset      int
	oldText     string
	replacement string
}

// runUnnecessaryExportAnalyzer resolves the analysis root, reports each finding, and optionally applies safe renames.
func runUnnecessaryExportAnalyzer(ctx context.Context, dir string, fix bool, output io.Writer) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve unnecessary-export analysis root: %w", err)
	}
	findings, err := findUnnecessaryExports(ctx, root)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		return nil
	}

	for _, finding := range findings {
		path, pathErr := filepath.Rel(root, finding.position.Filename)
		if pathErr != nil {
			path = finding.position.Filename
		}
		if finding.fixable {
			fmt.Fprintf(output, "%s:%d:%d: exported %s %s has no references outside package %s; unexport as %s\n",
				path, finding.position.Line, finding.position.Column, finding.kind, finding.key.name, finding.key.pkgPath, finding.newName)
			continue
		}
		fmt.Fprintf(output, "%s:%d:%d: exported %s %s has no references outside package %s; cannot unexport automatically: %s\n",
			path, finding.position.Line, finding.position.Column, finding.kind, finding.key.name, finding.key.pkgPath, finding.reason)
	}

	if !fix {
		return fmt.Errorf("%d unnecessary exported declarations found", len(findings))
	}
	if err := applyUnnecessaryExportFixes(findings); err != nil {
		return err
	}

	remaining, err := findUnnecessaryExports(ctx, root)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("%d unnecessary exported declarations remain after auto-fix", len(remaining))
	}
	return nil
}

// findUnnecessaryExports loads module packages, collects exported declarations and references, then marks safe unexported renames after collision checks.
func findUnnecessaryExports(ctx context.Context, dir string) ([]unnecessaryExport, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		Tests: true,
	}
	loaded, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages for unnecessary-export analysis: %w", err)
	}
	for _, pkg := range loaded {
		if len(pkg.Errors) != 0 {
			return nil, fmt.Errorf("load package %s for unnecessary-export analysis: %s", pkg.PkgPath, pkg.Errors[0])
		}
	}

	candidates := make(map[exportKey]*unnecessaryExport)
	packageScopes := make(map[string]*types.Scope)
	packageImportNames := make(map[string]map[string]bool)
	for _, pkg := range loaded {
		if !isMainModulePackage(pkg) || pkg.ID != pkg.PkgPath {
			continue
		}
		packageScopes[pkg.PkgPath] = pkg.Types.Scope()
		packageImportNames[pkg.PkgPath] = importedNames(pkg)
		generatedFiles := generatedSyntaxFiles(pkg)
		for ident, obj := range pkg.TypesInfo.Defs {
			if ident == nil || obj == nil || !obj.Exported() || obj.Pkg() == nil || obj.Parent() != obj.Pkg().Scope() {
				continue
			}
			kind, ok := packageLevelDeclarationKind(obj)
			if !ok {
				continue
			}
			position := pkg.Fset.PositionFor(ident.Pos(), false)
			if generatedFiles[position.Filename] {
				continue
			}
			key := exportKey{pkgPath: pkg.PkgPath, name: obj.Name()}
			candidate := &unnecessaryExport{
				key:       key,
				kind:      kind,
				newName:   unexportedName(obj.Name()),
				position:  position,
				locations: make(map[string]sourceEdit),
				fixable:   true,
			}
			addSourceEdit(candidate.locations, position, obj.Name(), candidate.newName)
			candidates[key] = candidate
		}
	}

	// Same-package tests share the package namespace and can introduce imports
	// that conflict with an otherwise safe production declaration rename.
	for _, pkg := range loaded {
		if !isMainModulePackage(pkg) || packageScopes[pkg.PkgPath] == nil {
			continue
		}
		for name := range importedNames(pkg) {
			packageImportNames[pkg.PkgPath][name] = true
		}
	}

	for _, pkg := range loaded {
		if !isMainModulePackage(pkg) || pkg.ID != pkg.PkgPath {
			continue
		}
		for _, file := range pkg.Syntax {
			if ast.IsGenerated(file) {
				continue
			}
			for _, commentGroup := range file.Comments {
				for _, comment := range commentGroup.List {
					for key, candidate := range candidates {
						if key.pkgPath == pkg.PkgPath {
							addCommentEdits(candidate.locations, pkg.Fset, comment, key.name, candidate.newName)
						}
					}
				}
			}
		}
	}

	externallyUsed := make(map[exportKey]bool)
	for _, pkg := range loaded {
		if pkg.TypesInfo == nil {
			continue
		}
		for ident, obj := range pkg.TypesInfo.Uses {
			if ident == nil || obj == nil || obj.Pkg() == nil {
				continue
			}
			key := exportKey{pkgPath: obj.Pkg().Path(), name: obj.Name()}
			candidate := candidates[key]
			if candidate == nil {
				if embedded, ok := obj.(*types.Var); ok && embedded.Embedded() {
					key = exportKey{pkgPath: embedded.Pkg().Path(), name: embedded.Name()}
					candidate = candidates[key]
				}
			}
			if candidate == nil {
				continue
			}
			if obj.Parent() != obj.Pkg().Scope() {
				embedded, ok := obj.(*types.Var)
				if !ok || !embedded.Embedded() {
					continue
				}
			}
			if pkg.PkgPath != key.pkgPath {
				externallyUsed[key] = true
				continue
			}
			addSourceEdit(candidate.locations, pkg.Fset.PositionFor(ident.Pos(), false), key.name, candidate.newName)
		}
	}

	findings := make([]unnecessaryExport, 0, len(candidates))
	requestedNames := make(map[string][]*unnecessaryExport)
	for key, candidate := range candidates {
		if externallyUsed[key] {
			continue
		}
		scope := packageScopes[key.pkgPath]
		if existing := scope.Lookup(candidate.newName); existing != nil {
			candidate.fixable = false
			candidate.reason = fmt.Sprintf("%s already exists", candidate.newName)
		} else if packageImportNames[key.pkgPath][candidate.newName] {
			candidate.fixable = false
			candidate.reason = fmt.Sprintf("%s is imported by a file in the package", candidate.newName)
		}
		requestedKey := key.pkgPath + "\x00" + candidate.newName
		requestedNames[requestedKey] = append(requestedNames[requestedKey], candidate)
		findings = append(findings, *candidate)
	}
	for _, sameName := range requestedNames {
		if len(sameName) < 2 {
			continue
		}
		for _, candidate := range sameName {
			candidate.fixable = false
			candidate.reason = fmt.Sprintf("multiple declarations would be named %s", candidate.newName)
			for i := range findings {
				if findings[i].key == candidate.key {
					findings[i] = *candidate
				}
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].position.Filename != findings[j].position.Filename {
			return findings[i].position.Filename < findings[j].position.Filename
		}
		return findings[i].position.Offset < findings[j].position.Offset
	})
	return findings, nil
}

func isMainModulePackage(pkg *packages.Package) bool {
	return pkg != nil && pkg.Module != nil && pkg.Module.Main && pkg.Types != nil && pkg.TypesInfo != nil
}

func importedNames(pkg *packages.Package) map[string]bool {
	names := make(map[string]bool)
	for _, file := range pkg.Syntax {
		for _, spec := range file.Imports {
			name := ""
			if spec.Name != nil {
				name = spec.Name.Name
			} else if imported, ok := pkg.TypesInfo.Implicits[spec].(*types.PkgName); ok {
				name = imported.Name()
			}
			if name != "" && name != "." && name != "_" {
				names[name] = true
			}
		}
	}
	return names
}

func generatedSyntaxFiles(pkg *packages.Package) map[string]bool {
	generated := make(map[string]bool)
	for _, file := range pkg.Syntax {
		if ast.IsGenerated(file) {
			generated[pkg.Fset.PositionFor(file.Pos(), false).Filename] = true
		}
	}
	return generated
}

func packageLevelDeclarationKind(obj types.Object) (string, bool) {
	switch obj := obj.(type) {
	case *types.Func:
		if obj.Signature().Recv() != nil {
			return "", false
		}
		return "function", true
	case *types.TypeName:
		return "type", true
	case *types.Const:
		return "constant", true
	case *types.Var:
		return "variable", true
	default:
		return "", false
	}
}

func unexportedName(name string) string {
	if suffix, ok := strings.CutPrefix(name, "OAuth"); ok {
		return "oauth" + suffix
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return name
	}
	upperEnd := 0
	for upperEnd < len(runes) && unicode.IsUpper(runes[upperEnd]) {
		upperEnd++
	}
	lowerEnd := upperEnd
	if upperEnd > 1 && upperEnd < len(runes) && unicode.IsLower(runes[upperEnd]) {
		lowerEnd--
	}
	if lowerEnd == 0 {
		lowerEnd = 1
	}
	for i := 0; i < lowerEnd; i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

func addCommentEdits(edits map[string]sourceEdit, fset *token.FileSet, comment *ast.Comment, oldName, newName string) {
	remaining := comment.Text
	consumed := 0
	for {
		index := strings.Index(remaining, oldName)
		if index < 0 {
			return
		}
		absoluteIndex := consumed + index
		beforeIsIdentifier := false
		if absoluteIndex > 0 {
			before, _ := utf8.DecodeLastRuneInString(comment.Text[:absoluteIndex])
			beforeIsIdentifier = isIdentifierRune(before)
		}
		afterIndex := absoluteIndex + len(oldName)
		afterIsIdentifier := false
		if afterIndex < len(comment.Text) {
			after, _ := utf8.DecodeRuneInString(comment.Text[afterIndex:])
			afterIsIdentifier = isIdentifierRune(after)
		}
		if !beforeIsIdentifier && !afterIsIdentifier {
			position := fset.PositionFor(comment.Pos(), false)
			position.Offset += absoluteIndex
			position.Column += absoluteIndex
			addSourceEdit(edits, position, oldName, newName)
		}
		advance := index + len(oldName)
		consumed += advance
		remaining = remaining[advance:]
	}
}

func isIdentifierRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func addSourceEdit(edits map[string]sourceEdit, position token.Position, oldName, newName string) {
	if position.Filename == "" || position.Offset < 0 {
		return
	}
	key := fmt.Sprintf("%s\x00%d", position.Filename, position.Offset)
	edits[key] = sourceEdit{offset: position.Offset, oldText: oldName, replacement: newName}
}

// applyUnnecessaryExportFixes groups safe edits by file, applies each unique offset from end to start, then preserves the original file mode.
func applyUnnecessaryExportFixes(findings []unnecessaryExport) error {
	byFile := make(map[string][]sourceEdit)
	for _, finding := range findings {
		if !finding.fixable {
			continue
		}
		for key, edit := range finding.locations {
			filename, _, _ := strings.Cut(key, "\x00")
			byFile[filename] = append(byFile[filename], edit)
		}
	}

	for filename, edits := range byFile {
		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read %s for unnecessary-export fixes: %w", filename, err)
		}
		sort.Slice(edits, func(i, j int) bool { return edits[i].offset > edits[j].offset })
		seenOffsets := make(map[int]bool)
		for _, edit := range edits {
			if seenOffsets[edit.offset] {
				continue
			}
			seenOffsets[edit.offset] = true
			end := edit.offset + len(edit.oldText)
			if edit.offset < 0 || end > len(data) || !bytes.Equal(data[edit.offset:end], []byte(edit.oldText)) {
				return fmt.Errorf("source changed while fixing %s at byte %d", filename, edit.offset)
			}
			data = append(data[:edit.offset], append([]byte(edit.replacement), data[end:]...)...)
		}
		info, err := os.Stat(filename)
		if err != nil {
			return fmt.Errorf("stat %s for unnecessary-export fixes: %w", filename, err)
		}
		if err := os.WriteFile(filename, data, info.Mode()); err != nil {
			return fmt.Errorf("write %s for unnecessary-export fixes: %w", filename, err)
		}
	}
	return nil
}
