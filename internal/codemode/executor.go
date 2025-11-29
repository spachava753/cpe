package codemode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// mcpSDKVersion is the version of the MCP SDK to use in generated go.mod
const mcpSDKVersion = "v1.1.0"

// ExecutionResult represents the outcome of code execution
type ExecutionResult struct {
	Output   string // Combined stdout/stderr
	ExitCode int    // Exit code from the process
}

// ExecuteCode creates a temporary sandbox, writes files, and executes LLM-generated Go code.
// It returns the execution result with combined output and exit code.
// The temp directory is cleaned up after execution.
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

	// Run go mod tidy
	tidyResult, err := runCommand(ctx, tempDir, "go", "mod", "tidy")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go mod tidy: %w", err)
	}
	if tidyResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   tidyResult.Output,
			ExitCode: tidyResult.ExitCode,
		}, nil
	}

	// Build the binary to get accurate exit codes (go run masks them)
	binaryPath := filepath.Join(tempDir, "program")
	buildResult, err := runCommand(ctx, tempDir, "go", "build", "-o", binaryPath, ".")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go build: %w", err)
	}
	if buildResult.ExitCode != 0 {
		// Compilation error
		return ExecutionResult{
			Output:   buildResult.Output,
			ExitCode: buildResult.ExitCode,
		}, nil
	}

	// Execute the built binary
	result, err := runCommand(ctx, tempDir, binaryPath)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running program: %w", err)
	}

	return result, nil
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
