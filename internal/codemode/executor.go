package codemode

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/template"
	"time"
	"unicode/utf8"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// gracePeriod is the time to wait after sending SIGINT before sending SIGKILL
	gracePeriod = 5 * time.Second
	// timeoutCancellationNoteTemplate is appended to output when executionTimeout triggers cancellation.
	timeoutCancellationNoteTemplate = "execution timed out after %d seconds; context was canceled because executionTimeout was reached."
	spilledOutputFilePattern        = "cpe-code-output-*.txt"
)

//go:embed maingen.go.tmpl
var mainTemplateSource string

// executionResult captures process output and exit metadata from sandboxed code execution.
// Output is combined stdout/stderr and may contain truncation metadata when large-output
// spilling is enabled.
type executionResult struct {
	Output   string           // Combined stdout/stderr
	ExitCode int              // Exit code from the process
	Content  []mcpsdk.Content // Multimedia content returned from Run()
}

// executeCode runs generated Go code in an isolated temporary module.
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
func (c *ExecuteGoCodeCallback) executeCode(ctx context.Context, llmCode string, timeout int) (executionResult, error) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "cpe-code-mode-*")
	if err != nil {
		return executionResult{}, fmt.Errorf("creating temp directory: %w", err)
	}
	cleanupDir := tempDir
	// TODO: do we need to really need to eval symlink?
	if realTempDir, err := filepath.EvalSymlinks(tempDir); err == nil {
		tempDir = realTempDir
	}
	defer os.RemoveAll(cleanupDir)

	// Generate and write main.go
	tmpl, err := template.New("main.go").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).Parse(mainTemplateSource)
	if err != nil {
		return executionResult{}, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ ContentOutputPath string }{ContentOutputPath: filepath.Join(tempDir, "content.json")}); err != nil {
		return executionResult{}, fmt.Errorf("executing template: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), buf.Bytes(), 0o644); err != nil {
		return executionResult{}, fmt.Errorf("writing main.go: %w", err)
	}

	// Write run.go (LLM-generated code)
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(llmCode), 0o644); err != nil {
		return executionResult{}, fmt.Errorf("writing run.go: %w", err)
	}

	// Auto-correct imports
	importNote, err := correctFileImports(tempDir, "run.go")
	if err != nil {
		return executionResult{}, fmt.Errorf("error correcting imports: %w", err)
	}

	// Run go mod tidy
	tidyResult, err := runCommand(ctx, tempDir, "go", "mod", "tidy")
	if err != nil {
		return executionResult{}, fmt.Errorf("running go mod tidy: %w", err)
	}
	tidyResult.Output += importNote
	tidyResult = maybeSpillLargeOutput(tidyResult, c.LargeOutputCharLimit)
	if tidyResult.ExitCode != 0 {
		return executionResult{
			Output:   tidyResult.Output,
			ExitCode: tidyResult.ExitCode,
		}, RecoverableError{Output: tidyResult.Output, ExitCode: tidyResult.ExitCode}
	}

	// Build the binary to get accurate exit codes (go run masks them)
	binaryPath := filepath.Join(tempDir, "program")
	buildResult, err := runCommand(ctx, tempDir, "go", "build", "-o", binaryPath, ".")
	if err != nil {
		return executionResult{}, fmt.Errorf("running go build: %w", err)
	}
	buildResult.Output += importNote
	buildResult = maybeSpillLargeOutput(buildResult, c.LargeOutputCharLimit)
	if buildResult.ExitCode != 0 {
		return executionResult{
			Output:   buildResult.Output,
			ExitCode: buildResult.ExitCode,
		}, RecoverableError{Output: buildResult.Output, ExitCode: buildResult.ExitCode}
	}

	// Execute the built binary with timeout and graceful shutdown.
	// Only build-time steps use the temporary workspace. The generated program
	// itself runs with the normal inherited environment.
	result, err := runProgramWithTimeout(ctx, binaryPath, timeout, nil)
	result.Output += importNote
	result = maybeSpillLargeOutput(result, c.LargeOutputCharLimit)
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
func runCommand(ctx context.Context, dir string, name string, args ...string) (executionResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()

	result := executionResult{
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
func runProgramWithTimeout(ctx context.Context, binaryPath string, timeoutSecs int, envOverrides map[string]string) (executionResult, error) {
	if err := ctx.Err(); err != nil {
		return executionResult{}, err
	}

	cmd := exec.CommandContext(context.WithoutCancel(ctx), binaryPath)
	cmd.Env = mergeEnv(os.Environ(), envOverrides)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return executionResult{}, err
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

	result := executionResult{
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
func maybeSpillLargeOutput(result executionResult, charLimit int) executionResult {
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
func classifyExitCode(result executionResult) error {
	if result.ExitCode == 0 {
		return nil
	}
	return RecoverableError{Output: result.Output, ExitCode: result.ExitCode}
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
