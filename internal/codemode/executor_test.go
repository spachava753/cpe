package codemode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpcpe "github.com/spachava753/cpe/internal/mcp"
)

func TestExecuteCode(t *testing.T) {
	tests := []struct {
		name          string
		llmCode       string
		wantExitCode  int
		wantOutput    string
		wantErrType   string // "none", "recoverable", "fatal", "other"
		validate      func(t *testing.T, result ExecutionResult, err error)
		cancelContext bool
	}{
		{
			name: "successful execution",
			llmCode: `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("Hello from generated code")
	return nil, nil
}
`,
			wantExitCode: 0,
			wantOutput:   "Hello from generated code\n",
			wantErrType:  "none",
		},
		{
			name: "compilation error returns RecoverableError",
			llmCode: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	this is not valid go code
	return nil, nil
}
`,
			wantExitCode: 1,
			wantErrType:  "recoverable",
			validate: func(t *testing.T, result ExecutionResult, err error) {
				if !strings.Contains(result.Output, "syntax error") {
					t.Errorf("Output = %q, want compilation error containing 'syntax error'", result.Output)
				}
				var recErr RecoverableError
				if !errors.As(err, &recErr) {
					t.Errorf("error type = %T, want RecoverableError", err)
				}
			},
		},
		{
			name: "Run returns error (exit 1) returns RecoverableError",
			llmCode: `package main

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, errors.New("something went wrong")
}
`,
			wantExitCode: 1,
			wantOutput:   "\nexecution error: something went wrong\n",
			wantErrType:  "recoverable",
			validate: func(t *testing.T, result ExecutionResult, err error) {
				var recErr RecoverableError
				if !errors.As(err, &recErr) {
					t.Fatalf("error type = %T, want RecoverableError", err)
				}
				if recErr.ExitCode != 1 {
					t.Errorf("RecoverableError.ExitCode = %d, want 1", recErr.ExitCode)
				}
			},
		},
		{
			name: "panic (exit 2) returns RecoverableError",
			llmCode: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	panic("intentional panic")
}
`,
			wantExitCode: 2,
			wantErrType:  "recoverable",
			validate: func(t *testing.T, result ExecutionResult, err error) {
				if !strings.Contains(result.Output, "panic: intentional panic") {
					t.Errorf("Output = %q, want panic message containing 'panic: intentional panic'", result.Output)
				}
				var recErr RecoverableError
				if !errors.As(err, &recErr) {
					t.Fatalf("error type = %T, want RecoverableError", err)
				}
				if recErr.ExitCode != 2 {
					t.Errorf("RecoverableError.ExitCode = %d, want 2", recErr.ExitCode)
				}
			},
		},
		{
			name: "fatalExit (exit 3) returns FatalExecutionError",
			llmCode: `package main

import (
	"context"
	"os"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("about to fatal exit")
	os.Exit(3)
	return nil, nil
}
`,
			wantExitCode: 3,
			wantErrType:  "fatal",
			validate: func(t *testing.T, result ExecutionResult, err error) {
				var fatalErr FatalExecutionError
				if !errors.As(err, &fatalErr) {
					t.Fatalf("error type = %T, want FatalExecutionError", err)
				}
				if !strings.Contains(fatalErr.Output, "about to fatal exit") {
					t.Errorf("FatalExecutionError.Output = %q, want to contain 'about to fatal exit'", fatalErr.Output)
				}
			},
		},
		{
			name: "multiple output lines",
			llmCode: `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("Line 1")
	fmt.Println("Line 2")
	fmt.Println("Line 3")
	return nil, nil
}
`,
			wantExitCode: 0,
			wantOutput:   "Line 1\nLine 2\nLine 3\n",
			wantErrType:  "none",
		},
		{
			name: "stderr and stdout captured",
			llmCode: `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Fprint(os.Stderr, "stderr output")
	fmt.Print("stdout output")
	return nil, nil
}
`,
			wantExitCode: 0,
			wantOutput:   "stderr outputstdout output",
			wantErrType:  "none",
		},
		{
			name: "context cancellation",
			llmCode: `package main

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	time.Sleep(10 * time.Second)
	return nil, nil
}
`,
			cancelContext: true,
			wantErrType:   "other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			result, err := ExecuteCode(ctx, nil, tt.llmCode, 30)

			// Verify error type
			switch tt.wantErrType {
			case "none":
				if err != nil {
					t.Fatalf("ExecuteCode() error = %v, want nil", err)
				}
			case "recoverable":
				var recErr RecoverableError
				if !errors.As(err, &recErr) {
					t.Fatalf("ExecuteCode() error type = %T, want RecoverableError", err)
				}
			case "fatal":
				var fatalErr FatalExecutionError
				if !errors.As(err, &fatalErr) {
					t.Fatalf("ExecuteCode() error type = %T, want FatalExecutionError", err)
				}
			case "other":
				if err == nil {
					t.Fatal("ExecuteCode() expected error, got nil")
				}
				return // Skip further checks for "other" errors
			}

			if result.ExitCode != tt.wantExitCode {
				t.Errorf("ExitCode = %d, want %d; output: %s", result.ExitCode, tt.wantExitCode, result.Output)
			}

			if tt.validate != nil {
				tt.validate(t, result, err)
			} else if tt.wantOutput != "" && result.Output != tt.wantOutput {
				t.Errorf("Output = %q, want %q", result.Output, tt.wantOutput)
			}
		})
	}
}

func TestExecuteCode_EmptyServers(t *testing.T) {
	ctx := context.Background()

	llmCode := `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("No tools needed")
	return nil, nil
}
`

	result, err := ExecuteCode(ctx, []ServerToolsInfo{}, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	want := "No tools needed\n"
	if result.Output != want {
		t.Errorf("Output = %q, want %q", result.Output, want)
	}
}

func TestExecuteCode_TimeoutGracefulExit(t *testing.T) {
	ctx := context.Background()

	// Code that responds to context cancellation (SIGINT triggers context.Done())
	llmCode := `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	<-ctx.Done()
	fmt.Println("graceful shutdown")
	return nil, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 1)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v", err)
	}

	// Process should exit cleanly after receiving SIGINT - no error
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	if !strings.Contains(result.Output, "graceful shutdown") {
		t.Errorf("Output = %q, want to contain 'graceful shutdown'", result.Output)
	}
}

func TestExecuteCode_TimeoutForcedKill(t *testing.T) {
	ctx := context.Background()

	// Code that ignores SIGINT and keeps running
	llmCode := `package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	// Ignore SIGINT
	signal.Ignore(os.Interrupt)
	time.Sleep(30 * time.Second)
	return nil, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 1)

	// Process killed with SIGKILL returns RecoverableError
	var recErr RecoverableError
	if !errors.As(err, &recErr) {
		t.Fatalf("ExecuteCode() error type = %T, want RecoverableError", err)
	}

	// Exit code -1 on Linux when killed by SIGKILL
	if result.ExitCode == 0 {
		t.Errorf("ExitCode = %d, want non-zero (killed); output: %s", result.ExitCode, result.Output)
	}
}

func TestExecuteCode_ParentContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Code that waits for context cancellation
	llmCode := `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	<-ctx.Done()
	fmt.Println("parent cancelled")
	return nil, nil
}
`

	// Cancel parent context after a short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	result, err := ExecuteCode(ctx, nil, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v", err)
	}

	// Process should exit cleanly after parent context cancelled - no error
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	if !strings.Contains(result.Output, "parent cancelled") {
		t.Errorf("Output = %q, want to contain 'parent cancelled'", result.Output)
	}
}

func TestClassifyExitCode(t *testing.T) {
	tests := []struct {
		name        string
		exitCode    int
		wantErrType string // "none", "recoverable", "fatal"
	}{
		{"exit 0 success", 0, "none"},
		{"exit 1 Run error", 1, "recoverable"},
		{"exit 2 panic", 2, "recoverable"},
		{"exit 3 fatal", 3, "fatal"},
		{"exit -1 SIGKILL", -1, "recoverable"},
		{"exit 127 command not found", 127, "recoverable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecutionResult{Output: "test output", ExitCode: tt.exitCode}
			err := classifyExitCode(result)

			switch tt.wantErrType {
			case "none":
				if err != nil {
					t.Errorf("classifyExitCode() = %v, want nil", err)
				}
			case "recoverable":
				var recErr RecoverableError
				if !errors.As(err, &recErr) {
					t.Errorf("classifyExitCode() error type = %T, want RecoverableError", err)
				}
				if recErr.ExitCode != tt.exitCode {
					t.Errorf("RecoverableError.ExitCode = %d, want %d", recErr.ExitCode, tt.exitCode)
				}
			case "fatal":
				var fatalErr FatalExecutionError
				if !errors.As(err, &fatalErr) {
					t.Errorf("classifyExitCode() error type = %T, want FatalExecutionError", err)
				}
			}
		})
	}
}

func TestErrorMessages(t *testing.T) {
	t.Run("RecoverableError", func(t *testing.T) {
		err := RecoverableError{Output: "some output", ExitCode: 1}
		want := "recoverable execution error (exit code 1): some output"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})

	t.Run("FatalExecutionError", func(t *testing.T) {
		err := FatalExecutionError{Output: "fatal output"}
		want := "fatal execution error: fatal output"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})
}

func TestExecuteCode_ToolTypesCompile(t *testing.T) {
	servers := []ServerToolsInfo{
		{
			ServerName: "test-server",
			Config:     mcpcpe.ServerConfig{Type: "stdio", Command: "test-cmd"},
			Tools: []*mcp.Tool{
				{
					Name:        "get_weather",
					Description: "Get weather for a city",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
					},
					OutputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"temperature": map[string]any{"type": "number"},
						},
					},
				},
			},
		},
	}

	mainGo, err := GenerateMainGo(servers, "/tmp/content.json")
	if err != nil {
		t.Fatalf("GenerateMainGo() error: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "cpe-compile-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error: %v", err)
	}
	defer os.RemoveAll(tempDir)

	goMod := `module test
go 1.24
require github.com/modelcontextprotocol/go-sdk v1.1.0
`
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("WriteFile(go.mod) error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("WriteFile(main.go) error: %v", err)
	}

	runGo := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	var _ GetWeatherInput
	var _ GetWeatherOutput
	return nil, nil
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(runGo), 0644); err != nil {
		t.Fatalf("WriteFile(run.go) error: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy error: %v\n%s", err, out)
	}

	cmd = exec.CommandContext(context.Background(), "go", "build", ".")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\n%s\n\nGenerated main.go:\n%s", err, out, mainGo)
	}
}

func TestExecuteCode_AutoCorrectImports(t *testing.T) {
	// Only run this test if goimports is installed
	ctx := context.Background()
	llmCode := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	// fmt is missing, but goimports should add it
	fmt.Println("Imports corrected")
	return nil, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v, output: %s", err, result.Output)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	if !strings.Contains(result.Output, "Imports in run.go were auto-corrected") {
		t.Errorf("Output = %q, want to contain auto-correction note", result.Output)
	}

	if !strings.Contains(result.Output, "Added: fmt") {
		t.Errorf("Output = %q, want to contain 'Added: fmt'", result.Output)
	}

	if !strings.Contains(result.Output, "Imports corrected") {
		t.Errorf("Output = %q, want to contain program output 'Imports corrected'", result.Output)
	}
}

func TestUnmarshalContent(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantLen     int
		wantErr     bool
		errContains string
		validate    func(t *testing.T, content []mcp.Content)
	}{
		{
			name:    "empty array",
			input:   "[]",
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "single text content",
			input:   `[{"type":"text","text":"Hello, world!"}]`,
			wantLen: 1,
			wantErr: false,
			validate: func(t *testing.T, content []mcp.Content) {
				tc, ok := content[0].(*mcp.TextContent)
				if !ok {
					t.Fatalf("expected *mcp.TextContent, got %T", content[0])
				}
				if tc.Text != "Hello, world!" {
					t.Errorf("Text = %q, want %q", tc.Text, "Hello, world!")
				}
			},
		},
		{
			name:    "single image content",
			input:   `[{"type":"image","data":"aGVsbG8=","mimeType":"image/png"}]`,
			wantLen: 1,
			wantErr: false,
			validate: func(t *testing.T, content []mcp.Content) {
				ic, ok := content[0].(*mcp.ImageContent)
				if !ok {
					t.Fatalf("expected *mcp.ImageContent, got %T", content[0])
				}
				if ic.MIMEType != "image/png" {
					t.Errorf("MIMEType = %q, want %q", ic.MIMEType, "image/png")
				}
				if string(ic.Data) != "hello" {
					t.Errorf("Data = %q, want %q", string(ic.Data), "hello")
				}
			},
		},
		{
			name:    "single audio content",
			input:   `[{"type":"audio","data":"d29ybGQ=","mimeType":"audio/wav"}]`,
			wantLen: 1,
			wantErr: false,
			validate: func(t *testing.T, content []mcp.Content) {
				ac, ok := content[0].(*mcp.AudioContent)
				if !ok {
					t.Fatalf("expected *mcp.AudioContent, got %T", content[0])
				}
				if ac.MIMEType != "audio/wav" {
					t.Errorf("MIMEType = %q, want %q", ac.MIMEType, "audio/wav")
				}
				if string(ac.Data) != "world" {
					t.Errorf("Data = %q, want %q", string(ac.Data), "world")
				}
			},
		},
		{
			name:    "mixed content types",
			input:   `[{"type":"text","text":"Description"},{"type":"image","data":"aW1n","mimeType":"image/jpeg"}]`,
			wantLen: 2,
			wantErr: false,
			validate: func(t *testing.T, content []mcp.Content) {
				tc, ok := content[0].(*mcp.TextContent)
				if !ok {
					t.Fatalf("content[0]: expected *mcp.TextContent, got %T", content[0])
				}
				if tc.Text != "Description" {
					t.Errorf("content[0].Text = %q, want %q", tc.Text, "Description")
				}
				ic, ok := content[1].(*mcp.ImageContent)
				if !ok {
					t.Fatalf("content[1]: expected *mcp.ImageContent, got %T", content[1])
				}
				if ic.MIMEType != "image/jpeg" {
					t.Errorf("content[1].MIMEType = %q, want %q", ic.MIMEType, "image/jpeg")
				}
			},
		},
		{
			name:        "malformed JSON",
			input:       "not valid json",
			wantErr:     true,
			errContains: "unmarshaling content array",
		},
		{
			name:        "invalid JSON in array",
			input:       `[{"type":"text",broken}]`,
			wantErr:     true,
			errContains: "unmarshaling content array",
		},
		{
			name:        "unknown content type",
			input:       `[{"type":"video","data":"abc"}]`,
			wantErr:     true,
			errContains: `unknown content type "video" at index 0`,
		},
		{
			name:        "missing type field",
			input:       `[{"text":"hello"}]`,
			wantErr:     true,
			errContains: `unknown content type ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := unmarshalContent([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Fatal("unmarshalContent() expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unmarshalContent() error = %v", err)
			}

			if len(content) != tt.wantLen {
				t.Errorf("len(content) = %d, want %d", len(content), tt.wantLen)
			}

			if tt.validate != nil {
				tt.validate(t, content)
			}
		})
	}
}

func TestExecuteCode_WithContent(t *testing.T) {
	ctx := context.Background()

	// Code that returns text content
	llmCode := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return []mcp.Content{
		&mcp.TextContent{Text: "Generated text content"},
	}, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v, output: %s", err, result.Output)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] type = %T, want *mcp.TextContent", result.Content[0])
	}

	if tc.Text != "Generated text content" {
		t.Errorf("Content[0].Text = %q, want %q", tc.Text, "Generated text content")
	}
}

func TestExecuteCode_WithImageContent(t *testing.T) {
	ctx := context.Background()

	// Code that returns image content with base64 encoded data
	llmCode := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return []mcp.Content{
		&mcp.ImageContent{
			Data:     []byte("fake image data"),
			MIMEType: "image/png",
		},
	}, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v, output: %s", err, result.Output)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	if len(result.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(result.Content))
	}

	ic, ok := result.Content[0].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("Content[0] type = %T, want *mcp.ImageContent", result.Content[0])
	}

	if ic.MIMEType != "image/png" {
		t.Errorf("Content[0].MIMEType = %q, want %q", ic.MIMEType, "image/png")
	}

	if string(ic.Data) != "fake image data" {
		t.Errorf("Content[0].Data = %q, want %q", string(ic.Data), "fake image data")
	}
}

func TestExecuteCode_WithMixedContent(t *testing.T) {
	ctx := context.Background()

	// Code that returns mixed content and also prints to stdout
	llmCode := `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("stdout output")
	return []mcp.Content{
		&mcp.TextContent{Text: "Text part"},
		&mcp.ImageContent{Data: []byte("img"), MIMEType: "image/jpeg"},
	}, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v, output: %s", err, result.Output)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	// Verify stdout was captured
	if !strings.Contains(result.Output, "stdout output") {
		t.Errorf("Output = %q, want to contain 'stdout output'", result.Output)
	}

	// Verify content
	if len(result.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(result.Content))
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] type = %T, want *mcp.TextContent", result.Content[0])
	}
	if tc.Text != "Text part" {
		t.Errorf("Content[0].Text = %q, want %q", tc.Text, "Text part")
	}

	ic, ok := result.Content[1].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("Content[1] type = %T, want *mcp.ImageContent", result.Content[1])
	}
	if ic.MIMEType != "image/jpeg" {
		t.Errorf("Content[1].MIMEType = %q, want %q", ic.MIMEType, "image/jpeg")
	}
}

func TestExecuteCode_NoContent(t *testing.T) {
	ctx := context.Background()

	// Code that returns nil content
	llmCode := `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("text only output")
	return nil, nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 30)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v, output: %s", err, result.Output)
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	// Content should be nil/empty
	if len(result.Content) != 0 {
		t.Errorf("len(Content) = %d, want 0", len(result.Content))
	}

	// But stdout should still be captured
	if !strings.Contains(result.Output, "text only output") {
		t.Errorf("Output = %q, want to contain 'text only output'", result.Output)
	}
}

func TestEnsureMCPImport(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantImport bool
	}{
		{
			name: "adds import when no imports exist",
			input: `package main

func Run() error {
	return nil
}
`,
			wantImport: true,
		},
		{
			name: "adds import to existing import block",
			input: `package main

import "fmt"

func Run() error {
	fmt.Println("hello")
	return nil
}
`,
			wantImport: true,
		},
		{
			name: "import already present",
			input: `package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`,
			wantImport: true,
		},
		{
			name: "adds to multi-import block",
			input: `package main

import (
	"context"
	"os"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	data, _ := os.ReadFile("image.png")
	return []mcp.Content{
		&mcp.ImageContent{Data: data, MIMEType: "image/png"},
	}, nil
}
`,
			wantImport: true,
		},
		{
			name: "syntax error - returns unchanged",
			input: `package main

import (
	"context"

func Run( { // syntax error
`,
			wantImport: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureMCPImport([]byte(tt.input))
			hasImport := strings.Contains(string(result), "github.com/modelcontextprotocol/go-sdk/mcp")
			if hasImport != tt.wantImport {
				t.Errorf("hasImport = %v, want %v\nresult:\n%s", hasImport, tt.wantImport, string(result))
			}
		})
	}
}

func TestEnsureMCPImport_Integration(t *testing.T) {
	code := `package main

import (
	"context"
	"os"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	data, _ := os.ReadFile("test.png")
	return []mcp.Content{
		&mcp.ImageContent{Data: data, MIMEType: "image/png"},
	}, nil
}
`

	result := ensureMCPImport([]byte(code))
	if !strings.Contains(string(result), "github.com/modelcontextprotocol/go-sdk/mcp") {
		t.Fatal("expected mcp import to be added")
	}

	ctx := context.Background()
	execResult, err := ExecuteCode(ctx, nil, string(result), 30)
	if err != nil {
		if strings.Contains(execResult.Output, "undefined: mcp") {
			t.Errorf("mcp import was not properly added:\n%s", execResult.Output)
		}
		// Other errors are acceptable (e.g., file not found at runtime)
	}
	// Verify compilation succeeded (exit code 0 or 1 for runtime error is fine)
	if execResult.ExitCode != 0 && execResult.ExitCode != 1 {
		if strings.Contains(execResult.Output, "undefined:") {
			t.Errorf("compilation failed with undefined symbol:\n%s", execResult.Output)
		}
	}
}
