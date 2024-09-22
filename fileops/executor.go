package fileops

import (
	"fmt"
	"github.com/spachava753/cpe/extract"
	"os"
	"path/filepath"
	"strings"
)

type OperationResult struct {
	Operation extract.Modification
	Error     error
}

func ExecuteFileOperations(modifications []extract.Modification) []OperationResult {
	// Check if modifications slice is empty
	if len(modifications) == 0 {
		fmt.Println("No modifications to apply.")
		return []OperationResult{}
	}

	// First pass: Validate operations
	validOperations, invalidOperations := validateOperations(modifications)

	// Second pass: Execute valid operations
	results := make([]OperationResult, 0, len(modifications))
	results = append(results, invalidOperations...)

	for _, op := range validOperations {
		err := executeOperation(op)
		results = append(results, OperationResult{Operation: op, Error: err})
	}

	// Print summary
	printSummary(results)

	return results
}

func validateOperations(modifications []extract.Modification) ([]extract.Modification, []OperationResult) {
	var validOperations []extract.Modification
	var invalidOperations []OperationResult

	for _, mod := range modifications {
		if err := validateOperation(mod); err != nil {
			invalidOperations = append(invalidOperations, OperationResult{Operation: mod, Error: err})
		} else {
			validOperations = append(validOperations, mod)
		}
	}

	return validOperations, invalidOperations
}

func validateOperation(mod extract.Modification) error {
	switch m := mod.(type) {
	case extract.ModifyCode:
		return validateModifyCode(m)
	case extract.RemoveFile:
		return validateRemoveFile(m)
	case extract.CreateFile:
		return validateCreateFile(m)
	default:
		return fmt.Errorf("unknown modification type")
	}
}

func validateModifyCode(m extract.ModifyCode) error {
	if !fileExists(m.Path) {
		return fmt.Errorf("file does not exist: %s", m.Path)
	}
	return nil
}

func validateRemoveFile(m extract.RemoveFile) error {
	if !fileExists(m.Path) {
		return fmt.Errorf("file does not exist: %s", m.Path)
	}
	return nil
}

func validateCreateFile(m extract.CreateFile) error {
	if fileExists(m.Path) {
		return fmt.Errorf("file already exists: %s", m.Path)
	}
	return validatePath(m.Path)
}

func validatePath(path string) error {
	cleanPath := filepath.Clean(path)

	// Check if the path is absolute
	if filepath.IsAbs(cleanPath) {
		return fmt.Errorf("invalid path: %s (absolute paths are not allowed)", path)
	}

	// Check if the path starts with ".." (parent directory)
	if strings.HasPrefix(cleanPath, "..") {
		return fmt.Errorf("invalid path: %s (must be within project directory)", path)
	}

	return nil
}

func executeOperation(mod extract.Modification) error {
	switch m := mod.(type) {
	case extract.ModifyCode:
		return executeModifyCode(m)
	case extract.RemoveFile:
		return executeRemoveFile(m)
	case extract.CreateFile:
		return executeCreateFile(m)
	default:
		return fmt.Errorf("unknown modification type")
	}
}

func executeModifyCode(m extract.ModifyCode) error {
	content, err := os.ReadFile(m.Path)
	if err != nil {
		return err
	}

	newContent := string(content)
	for _, mod := range m.Edits {
		newContent = strings.Replace(newContent, mod.Search, mod.Replace, -1)
	}

	return os.WriteFile(m.Path, []byte(newContent), 0644)
}

func executeRemoveFile(m extract.RemoveFile) error {
	return os.Remove(m.Path)
}

func executeCreateFile(m extract.CreateFile) error {
	dir := filepath.Dir(m.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	if err := os.WriteFile(m.Path, []byte(m.Content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", m.Path, err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func printSummary(results []OperationResult) {
	successful := 0
	failed := 0

	fmt.Println("\nOperation Summary:")
	for _, result := range results {
		if result.Error == nil {
			successful++
			fmt.Printf("✅ Success: %s - %s\n", result.Operation.Type(), getOperationDescription(result.Operation))
		} else {
			failed++
			fmt.Printf("❌ Failed: %s - %s - Error: %v\n", result.Operation.Type(), getOperationDescription(result.Operation), result.Error)
		}
	}

	fmt.Printf("\nTotal operations: %d\n", len(results))
	fmt.Printf("Successful: %d\n", successful)
	fmt.Printf("Failed: %d\n", failed)
}

func getOperationDescription(op extract.Modification) string {
	switch m := op.(type) {
	case extract.ModifyCode:
		return fmt.Sprintf("Modify %s", m.Path)
	case extract.RemoveFile:
		return fmt.Sprintf("Remove %s", m.Path)
	case extract.CreateFile:
		return fmt.Sprintf("Create %s", m.Path)
	default:
		return "Unknown operation"
	}
}
