package codemode

import (
	"context"
	"errors"
	"fmt"
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
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code after parent context cancellation, got %d", result.ExitCode)
	}

	timeoutNote := fmt.Sprintf(timeoutCancellationNoteTemplate, 10)
	if strings.Contains(result.Output, timeoutNote) {
		t.Fatalf("output unexpectedly contained timeout note:\n%s", result.Output)
	}
}

func TestRunProgramWithTimeout_DoesNotStartWhenParentContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	markerPath := filepath.Join(t.TempDir(), "started.txt")
	binaryPath := buildMarkerBinary(t, markerPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runProgramWithTimeout(ctx, binaryPath, 10, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	if _, statErr := os.Stat(markerPath); !errors.Is(statErr, os.ErrNotExist) {
		if statErr == nil {
			t.Fatalf("binary started unexpectedly and created %s", markerPath)
		}
		t.Fatalf("stat marker file: %v", statErr)
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

func buildMarkerBinary(t *testing.T, markerPath string) string {
	t.Helper()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	binaryPath := filepath.Join(tempDir, "program")

	source := fmt.Sprintf(`package main

import "os"

func main() {
	if err := os.WriteFile(%q, []byte("started"), 0644); err != nil {
		panic(err)
	}
}
`, markerPath)

	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatalf("writing marker source: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building marker binary: %v\n%s", err, string(output))
	}

	return binaryPath
}

func TestMaybeSpillLargeOutput_SpillsToDisk(t *testing.T) {
	t.Parallel()

	original := "abcdefghijklmnopqrstuvwxyz"
	result := maybeSpillLargeOutput(executionResult{Output: original}, 10)

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
	result := maybeSpillLargeOutput(executionResult{Output: original}, 100)

	if result.Output != original {
		t.Fatalf("output mismatch: got %q, want %q", result.Output, original)
	}
}

func TestMaybeSpillLargeOutput_PreviewsByCharactersForSingleLineOutput(t *testing.T) {
	t.Parallel()

	original := "0123456789abcdefghijklmnopqrstuvwxyz"
	result := maybeSpillLargeOutput(executionResult{Output: original}, 8)

	spillPath := extractSpillPath(t, result.Output)
	t.Cleanup(func() { _ = os.Remove(spillPath) })

	want := formatSpilledOutputMessage(len([]rune(original)), 8, "01234567", spillPath, nil)
	if result.Output != want {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", result.Output, want)
	}
}

func extractSpillPath(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		const prefix = "Full output stored at: "
		if after, ok := strings.CutPrefix(line, prefix); ok {
			path := strings.TrimSpace(after)
			if path == "" {
				t.Fatal("spill path was empty")
			}
			return path
		}
	}
	t.Fatalf("spill path not found in output: %q", output)
	return ""
}
