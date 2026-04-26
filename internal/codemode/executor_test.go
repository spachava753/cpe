package codemode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/modfile"
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

func buildFakeGoimportsBinary(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	binaryPath := filepath.Join(tempDir, "goimports")

	source := `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "-w" {
		fmt.Fprintf(os.Stderr, "unexpected args: %v", os.Args[1:])
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v", err)
		os.Exit(2)
	}
	if expected := os.Getenv("CPE_GOIMPORTS_EXPECT_DIR"); expected != "" && cwd != expected {
		fmt.Fprintf(os.Stderr, "cwd mismatch: got %q want %q", cwd, expected)
		os.Exit(2)
	}
	if expected := os.Getenv("CPE_GOIMPORTS_EXPECT_GOWORK"); expected != "" && os.Getenv("GOWORK") != expected {
		fmt.Fprintf(os.Stderr, "GOWORK mismatch: got %q want %q", os.Getenv("GOWORK"), expected)
		os.Exit(2)
	}

	if content := os.Getenv("CPE_GOIMPORTS_REWRITE_CONTENT"); content != "" {
		if err := os.WriteFile(os.Args[2], []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "rewrite file: %v", err)
			os.Exit(2)
		}
	}
}
`

	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatalf("writing fake goimports source: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building fake goimports binary: %v\n%s", err, string(output))
	}

	return binaryPath
}

func overrideGoimportsCommandForTest(t *testing.T, name string, args ...string) {
	t.Helper()

	original := goimportsCommand
	goimportsCommand = func() (string, []string) {
		return name, append([]string(nil), args...)
	}
	t.Cleanup(func() {
		goimportsCommand = original
	})
}

func TestAutoCorrectImports_UsesChildProcessEnvAndDir(t *testing.T) {
	helperPath := buildFakeGoimportsBinary(t)
	overrideGoimportsCommandForTest(t, helperPath)

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "run.go")
	original := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	_ = helpermod.Message()
	return nil, nil
}
`
	rewritten := `package main

import (
	"context"

	"example.com/helpermod"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	_ = helpermod.Message()
	return nil, nil
}
`
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("writing run.go: %v", err)
	}

	expectedDir := tempDir
	if realDir, err := filepath.EvalSymlinks(tempDir); err == nil {
		expectedDir = realDir
	}

	note := autoCorrectImports(context.Background(), tempDir, "run.go", map[string]string{
		"CPE_GOIMPORTS_EXPECT_DIR":      expectedDir,
		"CPE_GOIMPORTS_EXPECT_GOWORK":   "/tmp/cpe-test-go.work",
		"CPE_GOIMPORTS_REWRITE_CONTENT": rewritten,
		"GOWORK":                        "/tmp/cpe-test-go.work",
	})

	wantNote := "\n\nNote: Imports in run.go were auto-corrected.\n  Added: example.com/helpermod"
	if note != wantNote {
		t.Fatalf("note mismatch:\n got: %q\nwant: %q", note, wantNote)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading rewritten run.go: %v", err)
	}
	if string(data) != rewritten {
		t.Fatalf("rewritten run.go mismatch:\n got: %q\nwant: %q", string(data), rewritten)
	}
}

func TestAutoCorrectImports_DoesNotMutateProcessEnv(t *testing.T) {
	helperPath := buildFakeGoimportsBinary(t)
	overrideGoimportsCommandForTest(t, helperPath)
	originalProcessGOWORK := os.Getenv("GOWORK")

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "run.go")
	original := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("writing run.go: %v", err)
	}

	note := autoCorrectImports(context.Background(), tempDir, "run.go", map[string]string{
		"CPE_GOIMPORTS_EXPECT_GOWORK": "child-work",
		"GOWORK":                      "child-work",
	})
	if note != "" {
		t.Fatalf("expected empty note, got %q", note)
	}
	if got := os.Getenv("GOWORK"); got != originalProcessGOWORK {
		t.Fatalf("process GOWORK mutated: got %q want %q", got, originalProcessGOWORK)
	}
}

func TestGoimportsModuleVersionMatchesGoMod(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	goModPath := filepath.Join(repoRoot, "go.mod")

	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	parsed, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		t.Fatalf("parsing go.mod: %v", err)
	}

	var toolsVersion string
	for _, req := range parsed.Require {
		if req.Mod.Path == "golang.org/x/tools" {
			toolsVersion = req.Mod.Version
			break
		}
	}
	if toolsVersion == "" {
		t.Fatal("golang.org/x/tools requirement not found in go.mod")
	}
	if toolsVersion != goimportsModuleVersion {
		t.Fatalf("goimportsModuleVersion mismatch: got %q want %q", goimportsModuleVersion, toolsVersion)
	}
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

func TestExecuteCode_RuntimeDoesNotInheritWorkspaceForNestedGoCommands(t *testing.T) {
	t.Parallel()

	helperModuleDir := createLocalModule(t, t.TempDir(), "helpermod", "example.com/helpermod", `package helpermod

func Message() string {
	return "ok"
}
`)

	llmCode := `package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	root, err := os.MkdirTemp("", "nested-go-test-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)

	goMod := []byte("module example.com/unrelated\n\ngo 1.24\n")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), goMod, 0o644); err != nil {
		return nil, err
	}
	goTest := []byte("package unrelated\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n")
	if err := os.WriteFile(filepath.Join(root, "unrelated_test.go"), goTest, 0o644); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./...")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nested go test failed: %w\n%s", err, string(output))
	}

	fmt.Printf("helper=%s\ngo-test=ok\n", helpermod.Message())
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

	wantOutput := "helper=ok\ngo-test=ok\n\n\nNote: Imports in run.go were auto-corrected.\n  Added: example.com/helpermod"
	if result.Output != wantOutput {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", result.Output, wantOutput)
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

func TestPrepareWorkspaceFile_UsesHighestGoDirective(t *testing.T) {
	t.Parallel()

	tempModuleDir := t.TempDir()
	helperModuleDir := createLocalModuleWithGoVersion(t, t.TempDir(), "helpermod", "example.com/helpermod", "1.25", `package helpermod

func Message() string {
	return "ok"
}
`)

	workspacePath, _, workspaceGoVersion, err := prepareWorkspaceFile(tempModuleDir, ExecuteCodeOptions{
		LocalModulePaths: []string{helperModuleDir},
	}, "1.24")
	if err != nil {
		t.Fatalf("prepareWorkspaceFile returned error: %v", err)
	}
	if workspaceGoVersion != "1.25" {
		t.Fatalf("workspace go version mismatch: got %q, want %q", workspaceGoVersion, "1.25")
	}

	data, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("reading go.work: %v", err)
	}
	if !strings.Contains(string(data), "go 1.25") {
		t.Fatalf("go.work missing expected go version:\n%s", string(data))
	}
}

func TestNormalizeGoDirectiveVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "go prefixed", input: "go1.25.5", want: "1.25.5"},
		{name: "plain", input: "1.24", want: "1.24"},
		{name: "v prefixed", input: "v1.26.0", want: "1.26.0"},
		{name: "invalid", input: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeGoDirectiveVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeGoDirectiveVersion returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("version mismatch: got %q, want %q", got, tt.want)
			}
		})
	}
}

func createLocalModule(t *testing.T, root, dirName, modulePath, source string) string {
	return createLocalModuleWithGoVersion(t, root, dirName, modulePath, "1.24", source)
}

func createLocalModuleWithGoVersion(t *testing.T, root, dirName, modulePath, goVersion, source string) string {
	t.Helper()

	moduleDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("creating module dir: %v", err)
	}

	goMod := "module " + modulePath + "\n\ngo " + goVersion + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	if err := os.WriteFile(filepath.Join(moduleDir, "module.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("writing module source: %v", err)
	}

	return moduleDir
}
