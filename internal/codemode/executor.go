package codemode

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/tools/imports"
)

// mcpSDKVersion is the version of the MCP SDK to use in generated go.mod
const mcpSDKVersion = "v1.1.0"

// gracePeriod is the time to wait after sending SIGINT before sending SIGKILL
const gracePeriod = 5 * time.Second

// ExecutionResult represents the outcome of code execution
type ExecutionResult struct {
	Output   string // Combined stdout/stderr
	ExitCode int    // Exit code from the process
}

// ExecuteCode creates a temporary sandbox, writes files, and executes LLM-generated Go code.
// It returns the execution result with combined output and exit code.
// The temp directory is cleaned up after execution.
//
// Error classification:
//   - nil error with ExitCode 0: successful execution
//   - RecoverableError: compilation errors, Run() errors (exit 1), panics (exit 2), timeouts
//   - FatalExecutionError: exit code 3 from fatalExit() in generated code
//   - Other errors: infrastructure failures (temp dir, file writes, etc.)
func ExecuteCode(ctx context.Context, servers []ServerToolsInfo, llmCode string, timeoutSecs int) (ExecutionResult, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "cpe-code-mode-*")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Generate and write go.mod
	goMod := generateGoMod()
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing go.mod: %w", err)
	}

	// Generate and write main.go
	mainGo, err := GenerateMainGo(servers)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("generating main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(mainGo), 0644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing main.go: %w", err)
	}

	// Write run.go (LLM-generated code)
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(llmCode), 0644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing run.go: %w", err)
	}

	// Auto-correct imports
	importNote := autoCorrectImports(ctx, tempDir, "run.go")

	// Run go mod tidy
	tidyResult, err := runCommand(ctx, tempDir, "go", "mod", "tidy")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go mod tidy: %w", err)
	}
	tidyResult.Output += importNote
	if tidyResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   tidyResult.Output,
			ExitCode: tidyResult.ExitCode,
		}, RecoverableError(tidyResult)
	}

	// Build the binary to get accurate exit codes (go run masks them)
	binaryPath := filepath.Join(tempDir, "program")
	buildResult, err := runCommand(ctx, tempDir, "go", "build", "-o", binaryPath, ".")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go build: %w", err)
	}
	buildResult.Output += importNote
	if buildResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   buildResult.Output,
			ExitCode: buildResult.ExitCode,
		}, RecoverableError(buildResult)
	}

	// Execute the built binary with timeout and graceful shutdown
	result, err := runProgramWithTimeout(ctx, binaryPath, timeoutSecs)
	result.Output += importNote
	if err != nil {
		return result, err
	}

	return result, classifyExitCode(result)
}

// generateGoMod creates the go.mod file contents
func generateGoMod() string {
	return fmt.Sprintf(`module cpe-code-execution

go 1.24

require github.com/modelcontextprotocol/go-sdk %s
`, mcpSDKVersion)
}

// runCommand executes a command in the given directory and returns the result
func runCommand(ctx context.Context, dir string, name string, args ...string) (ExecutionResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	result := ExecutionResult{
		Output:   output.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., command not found, context cancelled)
			return result, err
		}
	}

	return result, nil
}

// runProgramWithTimeout executes a binary with timeout enforcement and graceful shutdown.
// On timeout or context cancellation, it sends SIGINT and waits gracePeriod for the process
// to exit gracefully before sending SIGKILL.
func runProgramWithTimeout(ctx context.Context, binaryPath string, timeoutSecs int) (ExecutionResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, binaryPath)

	// Custom cancel: send SIGINT for graceful shutdown instead of default SIGKILL.
	// Return os.ErrProcessDone to suppress context error when process exits cleanly.
	cmd.Cancel = func() error {
		cmd.Process.Signal(syscall.SIGINT)
		return os.ErrProcessDone
	}

	// Grace period: if process doesn't exit after SIGINT, Go sends SIGKILL
	cmd.WaitDelay = gracePeriod

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	result := ExecutionResult{
		Output:   output.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., binary not found)
			return result, err
		}
	}

	return result, nil
}

// classifyExitCode returns an appropriate error based on the execution result's exit code.
func classifyExitCode(result ExecutionResult) error {
	switch result.ExitCode {
	case 0:
		return nil
	case 1, 2:
		// Exit 1: Run() returned error; Exit 2: panic - both recoverable
		return RecoverableError(result)
	case 3:
		// Exit 3: fatalExit() called - unrecoverable
		return FatalExecutionError{Output: result.Output}
	default:
		// Other non-zero codes (e.g., -1 from SIGKILL) are recoverable
		return RecoverableError(result)
	}
}

// autoCorrectImports runs goimports (via golang.org/x/tools/imports) on the file
// and returns a notification message listing added/removed packages.
func autoCorrectImports(ctx context.Context, dir, filename string) string {
	filePath := filepath.Join(dir, filename)
	orig, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	origImports := extractImports(orig)

	// Process the file using golang.org/x/tools/imports
	newContent, err := imports.Process(filePath, orig, nil)
	if err != nil {
		// If processing fails (e.g. syntax errors), let the compiler catch it
		return ""
	}

	if bytes.Equal(orig, newContent) {
		return ""
	}

	if err := os.WriteFile(filePath, newContent, 0644); err != nil {
		return ""
	}

	newImports := extractImports(newContent)
	return formatImportChanges(filename, origImports, newImports)
}

// extractImports parses Go source and returns a set of import paths.
func extractImports(src []byte) map[string]bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return nil
	}

	imps := make(map[string]bool)
	for _, imp := range f.Imports {
		// Remove quotes from import path
		path := strings.Trim(imp.Path.Value, "`\"")
		imps[path] = true
	}
	return imps
}

// formatImportChanges generates a message describing which imports were added/removed.
func formatImportChanges(filename string, origImports, newImports map[string]bool) string {
	var added, removed []string

	for pkg := range newImports {
		if !origImports[pkg] {
			added = append(added, pkg)
		}
	}
	for pkg := range origImports {
		if !newImports[pkg] {
			removed = append(removed, pkg)
		}
	}

	if len(added) == 0 && len(removed) == 0 {
		return ""
	}

	sort.Strings(added)
	sort.Strings(removed)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n\nNote: Imports in %s were auto-corrected.", filename))
	if len(added) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Added: %s", strings.Join(added, ", ")))
	}
	if len(removed) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Removed: %s", strings.Join(removed, ", ")))
	}
	return sb.String()
}
