package codemode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRunProgramWithTimeout_AppendsTimeoutCancellationNote(t *testing.T) {
	t.Parallel()

	binaryPath := buildInterruptAwareBinary(t)

	result, err := runProgramWithTimeout(context.Background(), binaryPath, 1)
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

	result, err := runProgramWithTimeout(ctx, binaryPath, 10)
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

	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building test binary: %v\n%s", err, string(output))
	}

	return binaryPath
}
