package fileops

import (
	"fmt"
	"github.com/spachava753/cpe/parser"
	"os"
	"path/filepath"
	"strings"
)

type OperationResult struct {
	Operation parser.Modification
	Error     error
}

func ExecuteFileOperations(modifications []parser.Modification) []OperationResult {
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

func validateOperations(modifications []parser.Modification) ([]parser.Modification, []OperationResult) {
	var validOperations []parser.Modification
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

func validateOperation(mod parser.Modification) error {
	switch m := mod.(type) {
	case parser.ModifyCode:
		return validateModifyCode(m)
	case parser.RemoveFile:
		return validateRemoveFile(m)
	case parser.CreateFile:
		return validateCreateFile(m)
	case parser.RenameFile:
		return validateRenameFile(m)
	case parser.MoveFile:
		return validateMoveFile(m)
	case parser.CreateDirectory:
		return validateCreateDirectory(m)
	default:
		return fmt.Errorf("unknown modification type")
	}
}

func validateModifyCode(m parser.ModifyCode) error {
	if !fileExists(m.Path) {
		return fmt.Errorf("file does not exist: %s", m.Path)
	}
	return nil
}

func validateRemoveFile(m parser.RemoveFile) error {
	if !fileExists(m.Path) {
		return fmt.Errorf("file does not exist: %s", m.Path)
	}
	return nil
}

func validateCreateFile(m parser.CreateFile) error {
	if fileExists(m.Path) {
		return fmt.Errorf("file already exists: %s", m.Path)
	}
	return validatePath(m.Path)
}

func validateRenameFile(m parser.RenameFile) error {
	if !fileExists(m.OldPath) {
		return fmt.Errorf("file does not exist: %s", m.OldPath)
	}
	if fileExists(m.NewPath) {
		return fmt.Errorf("file already exists: %s", m.NewPath)
	}
	return validatePath(m.NewPath)
}

func validateMoveFile(m parser.MoveFile) error {
	if !fileExists(m.OldPath) {
		return fmt.Errorf("file does not exist: %s", m.OldPath)
	}
	if fileExists(m.NewPath) {
		return fmt.Errorf("file already exists: %s", m.NewPath)
	}
	return validatePath(m.NewPath)
}

func validateCreateDirectory(m parser.CreateDirectory) error {
	if fileExists(m.Path) {
		return fmt.Errorf("file or directory already exists: %s", m.Path)
	}
	return validatePath(m.Path)
}

func validatePath(path string) error {
	if !strings.HasPrefix(filepath.Clean(path), ".") {
		return fmt.Errorf("invalid path: %s (must be within project directory)", path)
	}
	return nil
}

func executeOperation(mod parser.Modification) error {
	switch m := mod.(type) {
	case parser.ModifyCode:
		return executeModifyCode(m)
	case parser.RemoveFile:
		return executeRemoveFile(m)
	case parser.CreateFile:
		return executeCreateFile(m)
	case parser.RenameFile:
		return executeRenameFile(m)
	case parser.MoveFile:
		return executeMoveFile(m)
	case parser.CreateDirectory:
		return executeCreateDirectory(m)
	default:
		return fmt.Errorf("unknown modification type")
	}
}

func executeModifyCode(m parser.ModifyCode) error {
	content, err := os.ReadFile(m.Path)
	if err != nil {
		return err
	}

	newContent := string(content)
	for _, mod := range m.Modifications {
		newContent = strings.Replace(newContent, mod.Search, mod.Replace, -1)
	}

	return os.WriteFile(m.Path, []byte(newContent), 0644)
}

func executeRemoveFile(m parser.RemoveFile) error {
	return os.Remove(m.Path)
}

func executeCreateFile(m parser.CreateFile) error {
	dir := filepath.Dir(m.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	if err := os.WriteFile(m.Path, []byte(m.Content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", m.Path, err)
	}
	return nil
}

func executeRenameFile(m parser.RenameFile) error {
	return os.Rename(m.OldPath, m.NewPath)
}

func executeMoveFile(m parser.MoveFile) error {
	dir := filepath.Dir(m.NewPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.Rename(m.OldPath, m.NewPath)
}

func executeCreateDirectory(m parser.CreateDirectory) error {
	return os.MkdirAll(m.Path, 0755)
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

func getOperationDescription(op parser.Modification) string {
	switch m := op.(type) {
	case parser.ModifyCode:
		return fmt.Sprintf("Modify %s", m.Path)
	case parser.RemoveFile:
		return fmt.Sprintf("Remove %s", m.Path)
	case parser.CreateFile:
		return fmt.Sprintf("Create %s", m.Path)
	case parser.RenameFile:
		return fmt.Sprintf("Rename %s to %s", m.OldPath, m.NewPath)
	case parser.MoveFile:
		return fmt.Sprintf("Move %s to %s", m.OldPath, m.NewPath)
	case parser.CreateDirectory:
		return fmt.Sprintf("Create directory %s", m.Path)
	default:
		return "Unknown operation"
	}
}
