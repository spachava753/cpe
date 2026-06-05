package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	goversion "go/version"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/tools/go/ast/astutil"
)

// mcpSDKVersion is the version of the MCP SDK to use in generated go.mod
const mcpSDKVersion = "v1.6.1"

// gracePeriod is the time to wait after sending SIGINT before sending SIGKILL
const gracePeriod = 5 * time.Second

// timeoutCancellationNoteTemplate is appended to output when executionTimeout triggers cancellation.
const timeoutCancellationNoteTemplate = "execution timed out after %d seconds; context was canceled because executionTimeout was reached."

// mcpSDKImport is the import path for the MCP SDK package
const mcpSDKImport = "github.com/modelcontextprotocol/go-sdk/mcp"

// goimportsModuleVersion must stay aligned with the golang.org/x/tools version in go.mod.
const goimportsModuleVersion = "v0.45.0"

const spilledOutputFilePattern = "cpe-code-output-*.txt"

var goDirectiveVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.\d+)?$`)

// ExecutionResult captures process output and exit metadata from sandboxed code execution.
// Output is combined stdout/stderr and may contain truncation metadata when large-output
// spilling is enabled.
type ExecutionResult struct {
	Output   string           // Combined stdout/stderr
	ExitCode int              // Exit code from the process
	Content  []mcpsdk.Content // Multimedia content returned from Run()
}

// ExecuteCodeOptions controls sandbox execution behavior.
// LargeOutputCharLimit <= 0 disables output spilling and preview truncation.
type ExecuteCodeOptions struct {
	TimeoutSeconds       int
	LargeOutputCharLimit int
}

var goimportsCommand = func() (string, []string) {
	return "go", []string{"run", "golang.org/x/tools/cmd/goimports@" + goimportsModuleVersion}
}

// ExecuteCode runs generated Go code in an isolated temporary module.
//
// Pipeline:
//   - create a temp module with generated main.go/run.go
//   - create go.mod/go.work and optional local module replacements
//   - run go mod tidy, go build, then execute the compiled binary
//   - deserialize content.json into Result.Content when execution succeeds
//
// Error classification:
//   - nil error with ExitCode 0: successful execution
//   - RecoverableError: compile failures, Run() errors, panics, timeouts, and other non-zero exits
//   - Other errors: infrastructure failures (temp dir, file writes, command launch failures)
func ExecuteCode(ctx context.Context, llmCode string, opts ExecuteCodeOptions) (ExecutionResult, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "cpe-code-mode-*")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("creating temp directory: %w", err)
	}
	cleanupDir := tempDir
	if realTempDir, err := filepath.EvalSymlinks(tempDir); err == nil {
		tempDir = realTempDir
	}
	defer os.RemoveAll(cleanupDir)

	// Generate and write main.go
	mainGo, err := GenerateMainGo(filepath.Join(tempDir, "content.json"))
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("generating main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		return ExecutionResult{}, fmt.Errorf("writing main.go: %w", err)
	}

	// Write run.go (LLM-generated code)
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(llmCode), 0o644); err != nil {
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
	tidyResult = maybeSpillLargeOutput(tidyResult, opts.LargeOutputCharLimit)
	if tidyResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   tidyResult.Output,
			ExitCode: tidyResult.ExitCode,
		}, RecoverableError{Output: tidyResult.Output, ExitCode: tidyResult.ExitCode}
	}

	// Build the binary to get accurate exit codes (go run masks them)
	binaryPath := filepath.Join(tempDir, "program")
	buildResult, err := runCommand(ctx, tempDir, "go", "build", "-o", binaryPath, ".")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("running go build: %w", err)
	}
	buildResult.Output += importNote
	buildResult = maybeSpillLargeOutput(buildResult, opts.LargeOutputCharLimit)
	if buildResult.ExitCode != 0 {
		return ExecutionResult{
			Output:   buildResult.Output,
			ExitCode: buildResult.ExitCode,
		}, RecoverableError{Output: buildResult.Output, ExitCode: buildResult.ExitCode}
	}

	// Execute the built binary with timeout and graceful shutdown.
	// Only build-time steps use the temporary workspace. The generated program
	// itself runs with the normal inherited environment.
	result, err := runProgramWithTimeout(ctx, binaryPath, opts.TimeoutSeconds, nil)
	result.Output += importNote
	result = maybeSpillLargeOutput(result, opts.LargeOutputCharLimit)
	if err != nil {
		return result, err
	}

	// Read content.json on successful execution (exit code 0)
	if result.ExitCode == 0 {
		contentPath := filepath.Join(tempDir, "content.json")
		if _, err := os.Stat(contentPath); err == nil {
			data, err := os.ReadFile(contentPath)
			if err != nil {
				return result, fmt.Errorf("reading content file: %w", err)
			}
			content, err := unmarshalContent(data)
			if err != nil {
				return result, fmt.Errorf("deserializing content: %w", err)
			}
			result.Content = content
		}
	}

	return result, classifyExitCode(result)
}

// generateGoMod creates the go.mod file contents.
func generateGoMod(goVersion string) string {
	return fmt.Sprintf(`module cpe-code-execution

go %s

require github.com/modelcontextprotocol/go-sdk %s
`, goVersion, mcpSDKVersion)
}

// normalizeGoDirectiveVersion accepts values like "go1.24.5", "v1.24", or
// "1.24" and returns the major.minor form accepted by all supported go.mod and
// go.work parsers.
func normalizeGoDirectiveVersion(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("go version must not be empty")
	}
	trimmed = strings.TrimPrefix(trimmed, "go")
	trimmed = strings.TrimPrefix(trimmed, "v")

	prefixed := "go" + trimmed
	if !goversion.IsValid(prefixed) {
		return "", fmt.Errorf("invalid go version %q", raw)
	}

	matches := goDirectiveVersionPattern.FindStringSubmatch(trimmed)
	if matches == nil {
		return "", fmt.Errorf("invalid go directive version %q", raw)
	}
	return matches[1] + "." + matches[2], nil
}

// compareGoDirectiveVersions compares normalized go directive versions.
func compareGoDirectiveVersions(left, right string) int {
	return goversion.Compare("go"+left, "go"+right)
}

// detectGoToolchainVersion reads GOVERSION from the active toolchain and normalizes
// it to a go.mod/go.work-compatible directive version.
func detectGoToolchainVersion(ctx context.Context) (string, error) {
	result, err := runCommand(ctx, "", "go", "env", "GOVERSION")
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New(strings.TrimSpace(result.Output))
	}

	version, err := normalizeGoDirectiveVersion(result.Output)
	if err != nil {
		return "", err
	}
	return version, nil
}

// mergeEnv applies KEY=VALUE overrides on top of base environment entries and
// returns a sorted environment slice for deterministic command invocation.
func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		merged[parts[0]] = parts[1]
	}
	maps.Copy(merged, overrides)

	result := make([]string, 0, len(merged))
	for key, value := range merged {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

// runCommand executes a command in dir with merged environment overrides.
// Exit errors are encoded in ExecutionResult.ExitCode; launch/context errors are returned directly.
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

// runProgramWithTimeout executes the compiled binary under executionTimeout semantics.
// On cancellation, it sends SIGINT first, allows gracePeriod for cleanup, then SIGKILL if needed.
// When executionTimeout triggers cancellation, a cancellation note is appended to stdout/stderr output.
func runProgramWithTimeout(ctx context.Context, binaryPath string, timeoutSecs int, envOverrides map[string]string) (ExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionResult{}, err
	}

	cmd := exec.CommandContext(context.WithoutCancel(ctx), binaryPath)
	cmd.Env = mergeEnv(os.Environ(), envOverrides)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return ExecutionResult{}, err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	timer := time.NewTimer(time.Duration(timeoutSecs) * time.Second)
	defer timer.Stop()

	timedOut := false
	var err error

	select {
	case err = <-waitCh:
	case <-timer.C:
		timedOut = true
		err = interruptThenWait(cmd, waitCh)
	case <-ctx.Done():
		err = interruptThenWait(cmd, waitCh)
	}

	result := ExecutionResult{
		Output:   output.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., binary not found, signaling failure)
			return result, err
		}
	}

	if timedOut {
		result.Output = appendTimeoutCancellationNote(result.Output, timeoutSecs)
	}

	return result, nil
}

func interruptThenWait(cmd *exec.Cmd, waitCh <-chan error) error {
	if cmd.Process == nil {
		return <-waitCh
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil && !isProcessDoneError(err) {
		return err
	}

	graceTimer := time.NewTimer(gracePeriod)
	defer graceTimer.Stop()

	select {
	case err := <-waitCh:
		return err
	case <-graceTimer.C:
		if err := cmd.Process.Kill(); err != nil && !isProcessDoneError(err) {
			return err
		}
		return <-waitCh
	}
}

func isProcessDoneError(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// appendTimeoutCancellationNote appends a stable timeout hint used by the agent loop
// to explain why execution stopped.
func appendTimeoutCancellationNote(output string, timeoutSecs int) string {
	note := fmt.Sprintf(timeoutCancellationNoteTemplate, timeoutSecs)
	if output == "" {
		return note
	}
	if strings.HasSuffix(output, "\n") {
		return output + note
	}
	return output + "\n" + note
}

// maybeSpillLargeOutput truncates oversized output to a preview and persists full output
// to a temp file so model context stays bounded while preserving debuggability.
func maybeSpillLargeOutput(result ExecutionResult, charLimit int) ExecutionResult {
	if charLimit <= 0 || result.Output == "" {
		return result
	}

	outputChars := utf8.RuneCountInString(result.Output)
	if outputChars <= charLimit {
		return result
	}

	preview := firstNChars(result.Output, charLimit)
	spillPath, spillErr := spillOutputToDisk(result.Output)
	result.Output = formatSpilledOutputMessage(outputChars, charLimit, preview, spillPath, spillErr)
	return result
}

// spillOutputToDisk writes full tool output to a temp file and returns the file path.
func spillOutputToDisk(output string) (string, error) {
	f, err := os.CreateTemp("", spilledOutputFilePattern)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.WriteString(output); err != nil {
		return "", err
	}

	return f.Name(), nil
}

// firstNChars returns a rune-safe prefix used for previewing truncated output.
func firstNChars(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// formatSpilledOutputMessage builds the warning shown to the model when output was truncated.
func formatSpilledOutputMessage(totalChars, charLimit int, preview, spillPath string, spillErr error) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("WARNING: tool result exceeded character limit (%d characters > %d).\n", totalChars, charLimit))

	if spillErr != nil {
		b.WriteString(fmt.Sprintf("Failed to persist full output to disk: %v\n", spillErr))
	} else {
		b.WriteString(fmt.Sprintf("Full output stored at: %s\n", spillPath))
	}

	b.WriteString(fmt.Sprintf("\nPreview (first %d characters):\n", charLimit))
	b.WriteString("---\n")
	b.WriteString(preview)
	if preview != "" && !strings.HasSuffix(preview, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("---\n")

	if spillErr == nil {
		b.WriteString("\nThe preview is truncated at the configured character limit. Read the file above for the remaining output.")
	} else {
		b.WriteString("\nThe preview is truncated at the configured character limit. Full output could not be persisted; re-run with more focused output.")
	}

	return b.String()
}

// classifyExitCode maps sandbox process exits to agent-facing error classes.
func classifyExitCode(result ExecutionResult) error {
	if result.ExitCode == 0 {
		return nil
	}
	return RecoverableError{Output: result.Output, ExitCode: result.ExitCode}
}

// autoCorrectImports runs goimports in a separate child process so workspace-
// specific env overrides stay isolated to that process.
func autoCorrectImports(ctx context.Context, dir, filename string) string {
	filePath := filepath.Join(dir, filename)
	orig, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	// Ensure correct mcp import before goimports to prevent wrong package resolution.
	preprocessed := ensureMCPImport(orig)
	origImports := extractImports(orig)

	tempFile, err := os.CreateTemp(dir, "cpe-goimports-*.go")
	if err != nil {
		return ""
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(preprocessed); err != nil {
		_ = tempFile.Close()
		return ""
	}
	if err := tempFile.Close(); err != nil {
		return ""
	}

	if err := runGoimportsProcess(ctx, dir, tempPath); err != nil {
		// If processing fails (e.g. syntax errors), let the compiler catch it.
		return ""
	}

	newContent, err := os.ReadFile(tempPath)
	if err != nil {
		return ""
	}
	if bytes.Equal(orig, newContent) {
		return ""
	}
	if err := os.WriteFile(filePath, newContent, 0o644); err != nil {
		return ""
	}

	newImports := extractImports(newContent)
	return formatImportChanges(filename, origImports, newImports)
}

func runGoimportsProcess(ctx context.Context, dir string, filePath string) error {
	name, args := goimportsCommand()
	args = append(args, "-w", filePath)

	result, err := runCommand(ctx, dir, name, args...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("goimports exited with code %d: %s", result.ExitCode, strings.TrimSpace(result.Output))
	}
	return nil
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

// contentTypeWrapper is used to peek at the type field during deserialization
type contentTypeWrapper struct {
	Type string `json:"type"`
}

// unmarshalContent deserializes a JSON array of MCP content items.
// It uses a two-phase approach: first peek at the type field, then unmarshal to the concrete type.
func unmarshalContent(data []byte) ([]mcpsdk.Content, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return nil, fmt.Errorf("unmarshaling content array: %w", err)
	}

	result := make([]mcpsdk.Content, 0, len(rawItems))
	for i, raw := range rawItems {
		var wrapper contentTypeWrapper
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return nil, fmt.Errorf("peeking type for item %d: %w", i, err)
		}

		var content mcpsdk.Content
		switch wrapper.Type {
		case "text":
			var tc mcpsdk.TextContent
			if err := json.Unmarshal(raw, &tc); err != nil {
				return nil, fmt.Errorf("unmarshaling text content at index %d: %w", i, err)
			}
			content = &tc
		case "image":
			var ic mcpsdk.ImageContent
			if err := json.Unmarshal(raw, &ic); err != nil {
				return nil, fmt.Errorf("unmarshaling image content at index %d: %w", i, err)
			}
			content = &ic
		case "audio":
			var ac mcpsdk.AudioContent
			if err := json.Unmarshal(raw, &ac); err != nil {
				return nil, fmt.Errorf("unmarshaling audio content at index %d: %w", i, err)
			}
			content = &ac
		default:
			return nil, fmt.Errorf("unknown content type %q at index %d", wrapper.Type, i)
		}
		result = append(result, content)
	}

	return result, nil
}

// ensureMCPImport adds the MCP SDK import if not already present.
// This prevents goimports from resolving mcp to the wrong package.
// The import will be removed by goimports if not actually used.
func ensureMCPImport(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	astutil.AddImport(fset, f, mcpSDKImport)

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, f); err != nil {
		return src
	}

	return buf.Bytes()
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
