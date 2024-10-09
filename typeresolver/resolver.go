package typeresolver

import (
	"fmt"
	"io/fs"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// ResolveTypeAndFunctionFiles resolves all type and function definitions used in the given files
func ResolveTypeAndFunctionFiles(selectedFiles []string, sourceFS fs.FS) (map[string]bool, error) {
	typeDefinitions := make(map[string]map[string]string)     // package.type -> file
	functionDefinitions := make(map[string]map[string]string) // package.function -> file
	usages := make(map[string]bool)
	imports := make(map[string]map[string]string) // file -> alias -> package

	parser := sitter.NewParser()
	goLang := sitter.NewLanguage(golang.Language())
	err := parser.SetLanguage(sitter.NewLanguage(golang.Language()))
	if err != nil {
		return nil, fmt.Errorf("error setting language: %w", err)
	}

	// Queries
	typeDefQuery, _ := sitter.NewQuery(goLang, `
                (type_declaration
                    (type_spec
                        name: (type_identifier) @type.definition))
                (type_alias
                    name: (type_identifier) @type.alias.definition)`)
	funcDefQuery, _ := sitter.NewQuery(goLang, `
                (function_declaration
                        name: (identifier) @function.definition)
                (method_declaration
                        name: (field_identifier) @method.definition)`)
	importQuery, _ := sitter.NewQuery(goLang, `
                (import_declaration
                    (import_spec_list
                        (import_spec
                            name: (_)? @import.name
                            path: (interpreted_string_literal) @import.path)))
                (import_declaration
                    (import_spec
                        name: (_)? @import.name
                        path: (interpreted_string_literal) @import.path))`)
	typeUsageQuery, _ := sitter.NewQuery(goLang, `
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
                ]`)
	funcUsageQuery, _ := sitter.NewQuery(goLang, `
                (call_expression
                    function: [
                        (identifier) @function.usage
                        (selector_expression
                            operand: [
                                (identifier) @package
                                (selector_expression)
                            ]?
                            field: (field_identifier) @method.usage)])`)

	// Parse all files in the source directory and collect type and function definitions
	err = fs.WalkDir(sourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			content, readErr := fs.ReadFile(sourceFS, path)
			if readErr != nil {
				return fmt.Errorf("error reading file %s: %w", path, readErr)
			}

			tree := parser.Parse(content, nil)
			defer tree.Close()

			// Extract package name
			pkgNameQuery, _ := sitter.NewQuery(goLang, `(package_clause (package_identifier) @package.name)`)
			defer pkgNameQuery.Close()
			pkgNameCursor := sitter.NewQueryCursor()
			defer pkgNameCursor.Close()
			matches := pkgNameCursor.Matches(pkgNameQuery, tree.RootNode(), content)
			pkgName := ""
			for match := matches.Next(); match != nil; match = matches.Next() {
				for _, capture := range match.Captures {
					pkgName = capture.Node.Utf8Text(content)
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
			defer typeCursor.Close()
			matches = typeCursor.Matches(typeDefQuery, tree.RootNode(), content)
			for match := matches.Next(); match != nil; match = matches.Next() {
				for _, capture := range match.Captures {
					typeName := capture.Node.Utf8Text(content)
					typeDefinitions[pkgName][typeName] = path
				}
			}

			// Process function definitions
			funcCursor := sitter.NewQueryCursor()
			defer funcCursor.Close()
			matches = funcCursor.Matches(funcDefQuery, tree.RootNode(), content)
			for match := matches.Next(); match != nil; match = matches.Next() {
				for _, capture := range match.Captures {
					functionDefinitions[pkgName][capture.Node.Utf8Text(content)] = path
				}
			}

			// Process imports
			importCursor := sitter.NewQueryCursor()
			defer importCursor.Close()
			matches = importCursor.Matches(importQuery, tree.RootNode(), content)
			for match := matches.Next(); match != nil; match = matches.Next() {
				var importName, importPath string
				for _, capture := range match.Captures {
					switch capture.Node.Kind() {
					case "identifier":
						importName = capture.Node.Utf8Text(content)
					case "interpreted_string_literal":
						importPath = strings.Trim(capture.Node.Utf8Text(content), "\"")
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
		content, readErr := fs.ReadFile(sourceFS, file)
		if readErr != nil {
			return nil, fmt.Errorf("error reading file %s: %w", file, readErr)
		}

		tree := parser.Parse(content, nil)

		// Process type usages
		typeUsageCursor := sitter.NewQueryCursor()
		matches := typeUsageCursor.Matches(typeUsageQuery, tree.RootNode(), content)
		for match := matches.Next(); match != nil; match = matches.Next() {
			var packageName, typeName string
			for _, capture := range match.Captures {
				switch capture.Node.Kind() {
				case "package_identifier":
					packageName = capture.Node.Utf8Text(content)
				case "type_identifier":
					typeName = capture.Node.Utf8Text(content)
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
		matches = funcUsageCursor.Matches(funcUsageQuery, tree.RootNode(), content)
		for match := matches.Next(); match != nil; match = matches.Next() {
			var packageName, funcName string
			for _, capture := range match.Captures {
				switch capture.Node.Kind() {
				case "identifier", "package":
					packageName = capture.Node.Utf8Text(content)
				case "field_identifier", "function.usage", "method.usage":
					funcName = capture.Node.Utf8Text(content)
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

		tree.Close()
	}

	return usages, nil
}
