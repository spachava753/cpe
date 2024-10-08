package typeresolver

import (
	"fmt"
	"golang.org/x/net/context"
	"io/fs"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// ResolveTypeAndFunctionFiles resolves all type and function definitions used in the given files
func ResolveTypeAndFunctionFiles(selectedFiles []string, sourceFS fs.FS) (map[string]bool, error) {
	typeDefinitions := make(map[string]map[string]string)     // package.type -> file
	functionDefinitions := make(map[string]map[string]string) // package.function -> file
	usages := make(map[string]bool)
	imports := make(map[string]map[string]string) // file -> alias -> package

	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	// Queries
	typeDefQuery, _ := sitter.NewQuery([]byte(`
                (type_declaration
                    (type_spec
                        name: (type_identifier) @type.definition))
                (type_alias
                    name: (type_identifier) @type.alias.definition)`), golang.GetLanguage())
	funcDefQuery, _ := sitter.NewQuery([]byte(`
                (function_declaration
                        name: (identifier) @function.definition)
                (method_declaration
                        name: (field_identifier) @method.definition)`), golang.GetLanguage())
	importQuery, _ := sitter.NewQuery([]byte(`
                (import_declaration
                    (import_spec_list
                        (import_spec
                            name: (_)? @import.name
                            path: (interpreted_string_literal) @import.path)))
                (import_declaration
                    (import_spec
                        name: (_)? @import.name
                        path: (interpreted_string_literal) @import.path))`), golang.GetLanguage())
	typeUsageQuery, _ := sitter.NewQuery([]byte(`
                [
                    (type_identifier) @type.usage
                    (qualified_type
                        package: (package_identifier) @package
                        name: (type_identifier) @type)
                    (generic_type
                        type: [
                            (type_identifier) @type.usage
                            (qualified_type
                                package: (package_identifier) @package
                                name: (type_identifier) @type)
                        ])
                ]`), golang.GetLanguage())
	funcUsageQuery, _ := sitter.NewQuery([]byte(`
                (call_expression
                    function: [
                        (identifier) @function.usage
                        (selector_expression
                            operand: [
                                (identifier) @package
                                (selector_expression)
                            ]?
                            field: (field_identifier) @method.usage)])`), golang.GetLanguage())

	// Parse all files in the source directory and collect type and function definitions
	err := fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			content, err := fs.ReadFile(sourceFS, path)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", path, err)
			}

			tree, err := parser.ParseCtx(context.Background(), nil, content)
			if err != nil {
				return fmt.Errorf("error parsing file %s: %w", path, err)
			}

			// Extract package name
			pkgNameQuery, _ := sitter.NewQuery([]byte(`(package_clause (package_identifier) @package.name)`), golang.GetLanguage())
			pkgNameCursor := sitter.NewQueryCursor()
			pkgNameCursor.Exec(pkgNameQuery, tree.RootNode())
			pkgName := ""
			if match, ok := pkgNameCursor.NextMatch(); ok {
				for _, capture := range match.Captures {
					pkgName = capture.Node.Content(content)
					break
				}
			}

			if _, ok := typeDefinitions[pkgName]; !ok {
				typeDefinitions[pkgName] = make(map[string]string)
			}
			if _, ok := functionDefinitions[pkgName]; !ok {
				functionDefinitions[pkgName] = make(map[string]string)
			}

			// Process type definitions and aliases
			typeCursor := sitter.NewQueryCursor()
			typeCursor.Exec(typeDefQuery, tree.RootNode())
			for {
				match, ok := typeCursor.NextMatch()
				if !ok {
					break
				}
				for _, capture := range match.Captures {
					typeName := capture.Node.Content(content)
					typeDefinitions[pkgName][typeName] = path
				}
			}

			// Process function definitions
			funcCursor := sitter.NewQueryCursor()
			funcCursor.Exec(funcDefQuery, tree.RootNode())
			for {
				match, ok := funcCursor.NextMatch()
				if !ok {
					break
				}
				for _, capture := range match.Captures {
					functionDefinitions[pkgName][capture.Node.Content(content)] = path
				}
			}

			// Process imports
			importCursor := sitter.NewQueryCursor()
			importCursor.Exec(importQuery, tree.RootNode())
			for {
				match, ok := importCursor.NextMatch()
				if !ok {
					break
				}
				var importName, importPath string
				for _, capture := range match.Captures {
					switch capture.Node.Type() {
					case "identifier":
						importName = capture.Node.Content(content)
					case "interpreted_string_literal":
						importPath = strings.Trim(capture.Node.Content(content), "\"")
					}
				}
				if importName == "" {
					parts := strings.Split(importPath, "/")
					importName = parts[len(parts)-1]
				}
				if imports[path] == nil {
					imports[path] = make(map[string]string)
				}
				imports[path][importName] = importPath
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking source directory: %w", err)
	}

	// Collect type and function usages for selected files
	for _, file := range selectedFiles {
		// Check if the file has a .go extension
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		content, err := fs.ReadFile(sourceFS, file)
		if err != nil {
			return nil, fmt.Errorf("error reading file %s: %w", file, err)
		}

		tree, err := parser.ParseCtx(context.Background(), nil, content)
		if err != nil {
			return nil, fmt.Errorf("error parsing file %s: %w", file, err)
		}

		// Process type usages
		typeUsageCursor := sitter.NewQueryCursor()
		typeUsageCursor.Exec(typeUsageQuery, tree.RootNode())
		for {
			match, ok := typeUsageCursor.NextMatch()
			if !ok {
				break
			}
			var packageName, typeName string
			for _, capture := range match.Captures {
				switch capture.Node.Type() {
				case "package_identifier":
					packageName = capture.Node.Content(content)
				case "type_identifier":
					typeName = capture.Node.Content(content)
				}
			}

			// Check if it's a local type
			for pkg, types := range typeDefinitions {
				if defFile, ok := types[typeName]; ok {
					usages[defFile] = true
					break
				}
				if packageName != "" && pkg == packageName {
					if defFile, ok := types[typeName]; ok {
						usages[defFile] = true
						break
					}
				}
			}

			// If not found as a local type, it might be an imported type
			if packageName != "" {
				if importPath, ok := imports[file][packageName]; ok {
					// Mark only the specific imported type as used
					for pkgName, types := range typeDefinitions {
						if strings.HasSuffix(importPath, pkgName) {
							if defFile, ok := types[typeName]; ok {
								usages[defFile] = true
								break
							}
						}
					}
				}
			}
		}

		// Process function usages
		funcUsageCursor := sitter.NewQueryCursor()
		funcUsageCursor.Exec(funcUsageQuery, tree.RootNode())
		for {
			match, ok := funcUsageCursor.NextMatch()
			if !ok {
				break
			}
			var packageName, funcName string
			for _, capture := range match.Captures {
				switch capture.Node.Type() {
				case "identifier", "package":
					packageName = capture.Node.Content(content)
				case "field_identifier", "function.usage", "method.usage":
					funcName = capture.Node.Content(content)
				}
			}

			// Check if it's a local function
			for pkg, funcs := range functionDefinitions {
				if defFile, ok := funcs[funcName]; ok {
					usages[defFile] = true
					break
				}
				if packageName != "" && pkg == packageName {
					if defFile, ok := funcs[funcName]; ok {
						usages[defFile] = true
						break
					}
				}
			}

			// If not found as a local function, it might be an imported function
			if packageName != "" {
				if importPath, ok := imports[file][packageName]; ok {
					// Mark only the specific imported function as used
					for pkgName, funcs := range functionDefinitions {
						if strings.HasSuffix(importPath, pkgName) {
							if defFile, ok := funcs[funcName]; ok {
								usages[defFile] = true
								break
							}
						}
					}
				}
			}
		}

		// Add files containing function definitions used in this file
		for _, funcs := range functionDefinitions {
			for funcName, defFile := range funcs {
				if strings.Contains(string(content), funcName) {
					usages[defFile] = true
				}
			}
		}

		// Add the selected file to the usages
		usages[file] = true
	}

	return usages, nil
}
