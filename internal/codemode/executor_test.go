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

const windowsGOOS = "windows"

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
		if runtime.GOOS == windowsGOOS {
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

func TestExecuteCodeReturnsRecoverableErrorForTidyFailure(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("uses a POSIX shell script to fake the go command")
	}

	binDir := t.TempDir()
	goEnvJSON, err := exec.CommandContext(
		t.Context(),
		"go",
		"env",
		"-json",
		"GO111MODULE",
		"GOFLAGS",
		"GOINSECURE",
		"GOMOD",
		"GOMODCACHE",
		"GONOPROXY",
		"GONOSUMDB",
		"GOPATH",
		"GOPROXY",
		"GOROOT",
		"GOSUMDB",
		"GOWORK",
	).Output()
	if err != nil {
		t.Fatalf("capture go env for fake go: %v", err)
	}
	fakeGo := filepath.Join(binDir, "go")
	fakeGoScript := `#!/bin/sh
if [ "$1" = "env" ] && [ "$2" = "-json" ]; then
	cat <<'CPE_GO_ENV_JSON'
` + string(goEnvJSON) + `
CPE_GO_ENV_JSON
	exit 0
fi
if [ "$1" = "env" ] && [ "$2" = "GOVERSION" ]; then
	echo go1.26.4
	exit 0
fi
if [ "$1" = "mod" ] && [ "$2" = "tidy" ]; then
	echo "tidy failed deterministically"
	exit 1
fi
echo "unexpected go invocation: $*" >&2
exit 1
`
	if err := os.WriteFile(fakeGo, []byte(fakeGoScript), 0o700); err != nil {
		t.Fatalf("write fake go: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	callback := &ExecuteGoCodeCallback{}
	result, err := callback.executeCode(t.Context(), `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`, 5)
	if err == nil {
		t.Fatal("executeCode() error = nil, want recoverable tidy failure")
	}
	if _, ok := errors.AsType[RecoverableError](err); !ok {
		t.Fatalf("executeCode() error = %T %[1]v, want RecoverableError", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("executeCode() exit code = %d, output = %q, want 1", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "tidy failed deterministically") {
		t.Fatalf("executeCode() output = %q, want fake tidy output", result.Output)
	}
}

func buildTestProgram(t *testing.T, source string) string {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write helper program: %v", err)
	}

	binaryPath := filepath.Join(dir, "program")
	if runtime.GOOS == windowsGOOS {
		binaryPath += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build helper program: %v\n%s", err, output)
	}
	return binaryPath
}
