package codemode

import (
	"fmt"
	"slices"

	"github.com/stoewer/go-strcase"
)

// ExecuteGoCodeToolName is the reserved tool name for code mode
const ExecuteGoCodeToolName = "execute_go_code"

// CheckReservedNameCollision checks if any tool name collides with "execute_go_code".
// Returns an error if a collision is found, nil otherwise.
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

// CheckPascalCaseCollision checks if any tool names produce duplicate Go function names
// when converted to pascal case. Returns an error describing the collision if found, nil otherwise.
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

// CheckToolNameCollisions runs both collision checks (reserved name and pascal case),
// failing fast on the first error encountered.
func CheckToolNameCollisions(toolNames []string) error {
	if err := CheckReservedNameCollision(toolNames); err != nil {
		return err
	}
	return CheckPascalCaseCollision(toolNames)
}
