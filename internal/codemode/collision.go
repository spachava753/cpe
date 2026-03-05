package codemode

import (
	"fmt"
	"slices"

	"github.com/stoewer/go-strcase"
)

// ExecuteGoCodeToolName is the reserved synthetic tool exposed by code mode.
// MCP servers must not provide a tool with this exact name.
const ExecuteGoCodeToolName = "execute_go_code"

// CheckReservedNameCollision rejects MCP tool sets that shadow execute_go_code,
// which would break registration of the sandbox executor tool.
func CheckReservedNameCollision(toolNames []string) error {
	if slices.Contains(toolNames, ExecuteGoCodeToolName) {
		return fmt.Errorf(
			"code mode collision: tool %q conflicts with reserved code mode tool name; "+
				"either exclude this tool using 'excludedTools' configuration or remove the MCP server providing it",
			ExecuteGoCodeToolName,
		)
	}
	return nil
}

// CheckPascalCaseCollision ensures tool names map to unique generated Go identifiers.
// Conversion uses strcase.UpperCamelCase and validates the shared function-var namespace.
func CheckPascalCaseCollision(toolNames []string) error {
	// Map of pascal case name to list of original tool names that produce it
	pascalToOriginal := make(map[string][]string)

	for _, name := range toolNames {
		// Convert tool name to Go function name (pascal case)
		pascalName := strcase.UpperCamelCase(name)
		pascalToOriginal[pascalName] = append(pascalToOriginal[pascalName], name)
	}

	// Check for any pascal case name with multiple original tools
	for pascalName, originals := range pascalToOriginal {
		if len(originals) > 1 {
			return fmt.Errorf(
				"code mode collision: tools %q and %q both convert to Go function name %q; "+
					"exclude one using 'excludedTools' configuration or remove one of the MCP servers providing them",
				originals[0], originals[1], pascalName,
			)
		}
	}

	return nil
}

// CheckToolNameCollisions runs reserved-name validation first, then identifier-collision
// validation, returning the first conflict found.
func CheckToolNameCollisions(toolNames []string) error {
	if err := CheckReservedNameCollision(toolNames); err != nil {
		return err
	}
	return CheckPascalCaseCollision(toolNames)
}
