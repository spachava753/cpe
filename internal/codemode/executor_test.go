package codemode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunProgramWithTimeout_AppendsTimeoutCancellationNote(t *testing.T) {
	t.Parallel()

	binaryPath := buildInterruptAwareBinary(t)

	result, err := runProgramWithTimeout(context.Background(), binaryPath, 1, nil)
	if err != nil {
		t.Fatalf("runProgramWithTimeout returned error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("exit code mismatch: got %d, want 1", result.ExitCode)
	}

	wantOutput := "execution error: context canceled\nexecution timed out after 1 seconds; context was canceled because executionTimeout was reached."
	if result.Output != wantOutput {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", result.Output, wantOutput)
	}
}

func TestRunProgramWithTimeout_DoesNotAppendTimeoutCancellationNoteWhenParentContextCanceled(t *testing.T) {
	t.Parallel()

	binaryPath := buildInterruptAwareBinary(t)

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(2*time.Second, cancel)
	defer timer.Stop()
	defer cancel()

	result, err := runProgramWithTimeout(ctx, binaryPath, 10, nil)
	if err != nil {
		t.Fatalf("runProgramWithTimeout returned error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("exit code mismatch: got %d, want 1", result.ExitCode)
	}

	wantOutput := "execution error: context canceled\n"
	if result.Output != wantOutput {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", result.Output, wantOutput)
	}
}

func buildInterruptAwareBinary(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	binaryPath := filepath.Join(tempDir, "program")

	source := `package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	<-ctx.Done()
	fmt.Printf("execution error: %v\n", ctx.Err())
	os.Exit(1)
}
`

	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatalf("writing test source: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building test binary: %v\n%s", err, string(output))
	}

	return binaryPath
}

func TestMaybeSpillLargeOutput_SpillsToDisk(t *testing.T) {
	t.Parallel()

	original := "abcdefghijklmnopqrstuvwxyz"
	result := maybeSpillLargeOutput(ExecutionResult{Output: original}, 10)

	spillPath := extractSpillPath(t, result.Output)
	t.Cleanup(func() { _ = os.Remove(spillPath) })

	wantOutput := formatSpilledOutputMessage(len([]rune(original)), 10, "abcdefghij", spillPath, nil)
	if result.Output != wantOutput {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", result.Output, wantOutput)
	}

	data, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("reading spill file: %v", err)
	}
	if string(data) != original {
		t.Fatalf("spill file content mismatch: got %q, want %q", string(data), original)
	}
}

func TestMaybeSpillLargeOutput_NoSpillWhenWithinLimit(t *testing.T) {
	t.Parallel()

	original := "small output"
	result := maybeSpillLargeOutput(ExecutionResult{Output: original}, 100)

	if result.Output != original {
		t.Fatalf("output mismatch: got %q, want %q", result.Output, original)
	}
}

func TestMaybeSpillLargeOutput_PreviewsByCharactersForSingleLineOutput(t *testing.T) {
	t.Parallel()

	original := "0123456789abcdefghijklmnopqrstuvwxyz"
	result := maybeSpillLargeOutput(ExecutionResult{Output: original}, 8)

	spillPath := extractSpillPath(t, result.Output)
	t.Cleanup(func() { _ = os.Remove(spillPath) })

	want := formatSpilledOutputMessage(len([]rune(original)), 8, "01234567", spillPath, nil)
	if result.Output != want {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", result.Output, want)
	}
}

func extractSpillPath(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		const prefix = "Full output stored at: "
		if strings.HasPrefix(line, prefix) {
			path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if path == "" {
				t.Fatal("spill path was empty")
			}
			return path
		}
	}
	t.Fatalf("spill path not found in output: %q", output)
	return ""
}

func TestExecuteCode_LocalModulePathsSupportsAutoImport(t *testing.T) {
	t.Parallel()

	helperModuleDir := createLocalModule(t, t.TempDir(), "helpermod", "example.com/helpermod", `package helpermod

func Message() string {
	return "ok"
}
`)

	llmCode := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	_ = helpermod.Message()
	return nil, nil
}
`

	result, err := ExecuteCode(context.Background(), nil, llmCode, ExecuteCodeOptions{
		TimeoutSeconds:   30,
		LocalModulePaths: []string{helperModuleDir},
	})
	if err != nil {
		t.Fatalf("ExecuteCode returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code mismatch: got %d, want 0", result.ExitCode)
	}
}

func TestExecuteCode_InvalidLocalModulePathFails(t *testing.T) {
	t.Parallel()

	notModuleDir := filepath.Join(t.TempDir(), "not-module")
	if err := os.MkdirAll(notModuleDir, 0o755); err != nil {
		t.Fatalf("creating non-module dir: %v", err)
	}

	llmCode := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`

	_, err := ExecuteCode(context.Background(), nil, llmCode, ExecuteCodeOptions{
		TimeoutSeconds:   30,
		LocalModulePaths: []string{notModuleDir},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wantPath := notModuleDir
	if realPath, realErr := filepath.EvalSymlinks(notModuleDir); realErr == nil {
		wantPath = realPath
	}
	want := "preparing go workspace: workspace module path is missing go.mod: " + wantPath
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func createLocalModule(t *testing.T, root, dirName, modulePath, source string) string {
	t.Helper()

	moduleDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("creating module dir: %v", err)
	}

	goMod := "module " + modulePath + "\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	if err := os.WriteFile(filepath.Join(moduleDir, "module.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("writing module source: %v", err)
	}

	return moduleDir
}
