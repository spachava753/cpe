package codemode

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/coder/acp-go-sdk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spachava753/cpe/internal/acp/xctx"
)

const (
	// timeoutCancellationNoteTemplate is appended to output when executionTimeout triggers cancellation.
	timeoutCancellationNoteTemplate = "execution timed out after %d seconds; context was canceled because executionTimeout was reached.\n"
	spilledOutputFilePattern        = "cpe-code-output-*.txt"
)

//go:embed maingen.go.tmpl
var mainTemplateSource string

//go:embed go.mod.tmpl
var goModTmplSource string

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
	defer os.RemoveAll(tempDir)

	slog.DebugContext(ctx, "compilation folder", slog.String("folder", tempDir))

	// Generate and write main.go
	mainFile, err := os.OpenFile(
		filepath.Join(tempDir, "main.go"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o777, // TODO: what is the right perms to use here?
	)
	if err != nil {
		return executionResult{}, fmt.Errorf("could not create main.go: %w", err)
	}
	mainTmpl, err := template.New("main.go").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).Parse(mainTemplateSource)
	if err != nil {
		return executionResult{}, fmt.Errorf("parsing template: %w", err)
	}

	if err := mainTmpl.Execute(
		mainFile,
		struct {
			ContentOutputPath string
		}{
			ContentOutputPath: filepath.Join(tempDir, "content.json"),
		},
	); err != nil {
		return executionResult{}, fmt.Errorf("executing template: %w", err)
	}

	// Write run.go (LLM-generated code)
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(llmCode), 0o644); err != nil {
		return executionResult{}, fmt.Errorf("writing run.go: %w", err)
	}

	slog.DebugContext(ctx, "templated go code")

	// Generate and write go.mod
	goModFile, err := os.OpenFile(
		filepath.Join(tempDir, "go.mod"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o777, // TODO: what is the right perms to use here?
	)
	if err != nil {
		return executionResult{}, fmt.Errorf("could not create main.go: %w", err)
	}
	goModTmpl, err := template.New("go.mod").Parse(goModTmplSource)
	if err != nil {
		return executionResult{}, fmt.Errorf("parsing template: %w", err)
	}
	systemGoVersion, err := c.systemGoVersion(ctx)
	if err != nil {
		return executionResult{}, err
	}
	if err := goModTmpl.Execute(
		goModFile,
		struct {
			GoVersion string
		}{
			GoVersion: systemGoVersion,
		}); err != nil {
		return executionResult{}, fmt.Errorf("executing template: %w", err)
	}

	slog.DebugContext(ctx, "wrote go.mod")

	// Auto-correct imports
	importNote, err := correctFileImports(tempDir, "run.go")
	if err != nil {
		return executionResult{}, fmt.Errorf("error correcting imports: %w", err)
	}

	slog.DebugContext(ctx, "corrected file imports")

	// Run go mod tidy
	tidyResult, err := c.runCommand(
		ctx,
		tempDir,
		"go",
		"mod",
		"tidy",
	)
	if err != nil {
		return executionResult{}, fmt.Errorf("running go mod tidy: %w", err)
	}
	tidyResult.Output += importNote
	if tidyResult.ExitCode != 0 {
		return executionResult{
			Output:   tidyResult.Output,
			ExitCode: tidyResult.ExitCode,
		}, RecoverableError{Output: tidyResult.Output, ExitCode: tidyResult.ExitCode}
	}

	slog.DebugContext(ctx, "ran go mod tidy")

	// Build the binary to get accurate exit codes (go run masks them)
	binaryPath := filepath.Join(tempDir, "program")
	buildResult, err := c.runCommand(ctx, tempDir, "go", "build", "-o", binaryPath, ".")
	if err != nil {
		return executionResult{}, fmt.Errorf("running go build: %w", err)
	}

	slog.DebugContext(ctx, "ran generated program")

	buildResult.Output += importNote
	if buildResult.ExitCode != 0 {
		return executionResult{
			Output:   buildResult.Output,
			ExitCode: buildResult.ExitCode,
		}, RecoverableError{Output: buildResult.Output, ExitCode: buildResult.ExitCode}
	}

	// Execute the built binary with timeout and graceful shutdown.
	// Only build-time steps use the temporary workspace. The generated program
	// itself runs with the normal inherited environment.
	result, err := c.runProgramWithTimeout(ctx, binaryPath, timeout)
	result.Output = importNote + result.Output
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

// systemGoVersion returns the version of the go executable resolved from PATH.
// The returned value is normalized for go.mod directives, for example "1.26.0".
func (c *ExecuteGoCodeCallback) systemGoVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOVERSION")

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("getting system go version: %w: %s", err, strings.TrimSpace(output.String()))
	}

	version := strings.TrimSpace(output.String())
	if version == "" {
		return "", fmt.Errorf("getting system go version: go env GOVERSION returned empty output")
	}
	return strings.TrimPrefix(version, "go"), nil
}

// runCommand executes a command in dir with merged environment overrides.
// Exit errors are encoded in ExecutionResult.ExitCode; launch/context errors are returned directly.
func (c *ExecuteGoCodeCallback) runCommand(ctx context.Context, dir string, name string, args ...string) (executionResult, error) {
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
func (c *ExecuteGoCodeCallback) runProgramWithTimeout(
	ctx context.Context,
	binaryPath string,
	timeoutSecs int,
) (executionResult, error) {
	if err := ctx.Err(); err != nil {
		return executionResult{}, err
	}

	creatTermResp, err := c.Conn.CreateTerminal(ctx, acp.CreateTerminalRequest{
		Command:         binaryPath,
		Cwd:             new(c.Cwd),
		OutputByteLimit: &c.LargeOutputCharLimit,
		SessionId:       c.SessionId,
	})
	if err != nil {
		return executionResult{}, err
	}

	termId := creatTermResp.TerminalId

	defer func() {
		// since context *could* canceled here, we need to create
		// a new context that isn't cancelled, but still has a
		// deadline so release request doesn't take forever
		// if stuck
		rctx := context.WithoutCancel(ctx)
		var cancel context.CancelFunc
		rctx, cancel = context.WithTimeout(rctx, 1*time.Second)
		_, err := c.Conn.ReleaseTerminal(rctx, acp.ReleaseTerminalRequest{
			SessionId:  c.SessionId,
			TerminalId: termId,
		})
		cancel()
		if err != nil {
			slog.ErrorContext(context.WithoutCancel(ctx), "could not release terminal", slog.Any("err", err))
		}
	}()

	// send in progress update for toolcall
	c.Conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: c.SessionId,
		Update: acp.UpdateToolCall(
			xctx.ToolCallIdFrom(ctx),
			acp.WithUpdateKind(acp.ToolKindExecute),
			acp.WithUpdateStatus(acp.ToolCallStatusInProgress),
			acp.WithUpdateContent([]acp.ToolCallContent{
				acp.ToolTerminalRef(termId),
			}),
		),
	})

	errChan := make(chan error)
	go func() {
		_, err := c.Conn.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{
			SessionId:  c.SessionId,
			TerminalId: termId,
		})
		errChan <- err
		close(errChan)
	}()

	var timedOut, killTerminal bool
	select {
	case <-time.After(time.Duration(timeoutSecs) * time.Second):
		timedOut = true
		killTerminal = true
		slog.InfoContext(
			ctx,
			"execution timed out",
			slog.Int("timeout", timeoutSecs),
		)
	case <-ctx.Done():
		err = ctx.Err()
		killTerminal = true
		slog.DebugContext(ctx, "context canceled")
	case err = <-errChan:
		// this is the only case where we don't need to kill
		// the terminal, but we did get an error trying to
		// wait for terminal to exit, unrecoverable
		if err != nil {
			return executionResult{}, err
		}
	}

	// TODO: create synctest tests to test concurrency semantics here
	if killTerminal {
		slog.DebugContext(ctx, "killing terminal")
		// since context *could* canceled here, we need to create
		// a new context that isn't cancelled, but still has a
		// deadline so kill terminal request doesn't take forever
		// if stuck
		kctx := context.WithoutCancel(ctx)
		var cancel context.CancelFunc
		kctx, cancel = context.WithTimeout(kctx, 1*time.Second)
		_, err = c.Conn.KillTerminal(kctx, acp.KillTerminalRequest{
			SessionId:  c.SessionId,
			TerminalId: termId,
		})
		cancel()
		if err != nil {
			return executionResult{}, err
		}

		// because we had to forcefully kill the terminal due
		// to context cancel or timeout, make sure that the
		// terminal wait goroutine returns
		if err := <-errChan; err != nil {
			return executionResult{}, err
		}
	}

	// since we killed the terminal, or it finished, we can get the output
	termOutputResp, err := c.Conn.TerminalOutput(ctx, acp.TerminalOutputRequest{
		SessionId:  c.SessionId,
		TerminalId: termId,
	})
	if err != nil {
		return executionResult{}, err
	}

	var result executionResult
	if termOutputResp.ExitStatus != nil &&
		termOutputResp.ExitStatus.ExitCode != nil {
		result.ExitCode = *termOutputResp.ExitStatus.ExitCode
	}

	var sb strings.Builder

	if timedOut {
		fmt.Fprintf(&sb, timeoutCancellationNoteTemplate, timeoutSecs)
		sb.WriteRune('\n')
	}
	if termOutputResp.Truncated {
		sb.WriteString("NOTE: output beginning was truncated\n\n")
	}

	sb.WriteString(termOutputResp.Output)

	result.Output = sb.String()

	return result, nil
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
