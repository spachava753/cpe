package codemode

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunProgramWithTimeoutWithoutTerminal(t *testing.T) {
	t.Parallel()

	t.Run("runs local program", func(t *testing.T) {
		t.Parallel()

		binaryPath := buildTestProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	data, err := os.ReadFile("sentinel.txt")
	if err != nil {
		fmt.Printf("read error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}
`)
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "sentinel.txt"), []byte("from-cwd"), 0o600); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}

		callback := &ExecuteGoCodeCallback{Cwd: workDir}
		result, err := callback.runProgramWithTimeout(t.Context(), binaryPath, 5)
		if err != nil {
			t.Fatalf("runProgramWithTimeout() error = %v, want nil", err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("runProgramWithTimeout() exit code = %d, output = %q, want 0", result.ExitCode, result.Output)
		}
		if result.Output != "from-cwd" {
			t.Fatalf("runProgramWithTimeout() output = %q, want %q", result.Output, "from-cwd")
		}
	})

	t.Run("reports timeout", func(t *testing.T) {
		t.Parallel()

		binaryPath := buildTestProgram(t, `package main

import "time"

func main() {
	time.Sleep(10 * time.Second)
}
`)

		callback := &ExecuteGoCodeCallback{}
		result, err := callback.runProgramWithTimeout(t.Context(), binaryPath, 1)
		if err != nil {
			t.Fatalf("runProgramWithTimeout() error = %v, want nil", err)
		}
		if result.ExitCode == 0 {
			t.Fatalf("runProgramWithTimeout() exit code = 0, output = %q, want non-zero", result.Output)
		}
		if !strings.Contains(result.Output, "execution timed out after 1 seconds") {
			t.Fatalf("runProgramWithTimeout() output = %q, want timeout note", result.Output)
		}
	})

	t.Run("truncates output tail", func(t *testing.T) {
		t.Parallel()

		binaryPath := buildTestProgram(t, `package main

import "fmt"

func main() {
	fmt.Print("abcdefghijklmnopqrstuvwxyz")
}
`)

		callback := &ExecuteGoCodeCallback{LargeOutputCharLimit: 10}
		result, err := callback.runProgramWithTimeout(t.Context(), binaryPath, 5)
		if err != nil {
			t.Fatalf("runProgramWithTimeout() error = %v, want nil", err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("runProgramWithTimeout() exit code = %d, output = %q, want 0", result.ExitCode, result.Output)
		}
		if !strings.Contains(result.Output, "NOTE: output beginning was truncated") {
			t.Fatalf("runProgramWithTimeout() output = %q, want truncation note", result.Output)
		}
		if !strings.HasSuffix(result.Output, "qrstuvwxyz") {
			t.Fatalf("runProgramWithTimeout() output = %q, want retained output tail", result.Output)
		}
		if strings.Contains(result.Output, "abcdef") {
			t.Fatalf("runProgramWithTimeout() output = %q, want beginning truncated", result.Output)
		}
	})

	t.Run("kills descendant processes", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("uses POSIX shell and process group semantics")
		}
		t.Parallel()

		binaryPath := buildTestProgram(t, `package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	cmd := exec.Command("sh", "-c", "trap '' INT; sleep 3; printf survived > survived.txt")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Printf("spawn error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("spawned child")
	time.Sleep(10 * time.Second)
}
`)
		workDir := t.TempDir()

		callback := &ExecuteGoCodeCallback{Cwd: workDir}
		result, err := callback.runProgramWithTimeout(t.Context(), binaryPath, 1)
		if err != nil {
			t.Fatalf("runProgramWithTimeout() error = %v, want nil", err)
		}
		if result.ExitCode == 0 {
			t.Fatalf("runProgramWithTimeout() exit code = 0, output = %q, want non-zero", result.Output)
		}

		time.Sleep(4 * time.Second)
		if _, err := os.Stat(filepath.Join(workDir, "survived.txt")); err == nil {
			t.Fatal("descendant process survived timeout cleanup")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat survived marker: %v", err)
		}
	})
}

func buildTestProgram(t *testing.T, source string) string {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write helper program: %v", err)
	}

	binaryPath := filepath.Join(dir, "program")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build helper program: %v\n%s", err, output)
	}
	return binaryPath
}
