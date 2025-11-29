package codemode

import (
	"context"
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
		validate      func(t *testing.T, result ExecutionResult)
		wantErr       bool
		cancelContext bool
	}{
		{
			name: "successful execution",
			llmCode: `package main

import (
	"context"
	"fmt"
)

func Run(ctx context.Context) error {
	fmt.Println("Hello from generated code")
	return nil
}
`,
			wantExitCode: 0,
			wantOutput:   "Hello from generated code\n",
		},
		{
			name: "compilation error",
			llmCode: `package main

import "context"

func Run(ctx context.Context) error {
	this is not valid go code
	return nil
}
`,
			wantExitCode: 1,
			validate: func(t *testing.T, result ExecutionResult) {
				// Compilation errors include dynamic file paths, so we verify key parts
				if !strings.Contains(result.Output, "syntax error") {
					t.Errorf("Output = %q, want compilation error containing 'syntax error'", result.Output)
				}
			},
		},
		{
			name: "Run returns error",
			llmCode: `package main

import (
	"context"
	"errors"
)

func Run(ctx context.Context) error {
	return errors.New("something went wrong")
}
`,
			wantExitCode: 1,
			wantOutput:   "\nexecution error: something went wrong\n",
		},
		{
			name: "panic",
			llmCode: `package main

import "context"

func Run(ctx context.Context) error {
	panic("intentional panic")
}
`,
			wantExitCode: 2,
			validate: func(t *testing.T, result ExecutionResult) {
				// Panic output includes stack traces with dynamic addresses
				if !strings.Contains(result.Output, "panic: intentional panic") {
					t.Errorf("Output = %q, want panic message containing 'panic: intentional panic'", result.Output)
				}
			},
		},
		{
			name: "multiple output lines",
			llmCode: `package main

import (
	"context"
	"fmt"
)

func Run(ctx context.Context) error {
	fmt.Println("Line 1")
	fmt.Println("Line 2")
	fmt.Println("Line 3")
	return nil
}
`,
			wantExitCode: 0,
			wantOutput:   "Line 1\nLine 2\nLine 3\n",
		},
		{
			name: "stderr and stdout captured",
			llmCode: `package main

import (
	"context"
	"fmt"
	"os"
)

func Run(ctx context.Context) error {
	fmt.Fprint(os.Stderr, "stderr output")
	fmt.Print("stdout output")
	return nil
}
`,
			wantExitCode: 0,
			wantOutput:   "stderr outputstdout output",
		},
		{
			name: "context cancellation",
			llmCode: `package main

import (
	"context"
	"time"
)

func Run(ctx context.Context) error {
	time.Sleep(10 * time.Second)
	return nil
}
`,
			cancelContext: true,
			wantErr:       true,
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

			if tt.wantErr {
				if err == nil {
					t.Error("ExecuteCode() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ExecuteCode() error: %v", err)
			}

			if result.ExitCode != tt.wantExitCode {
				t.Errorf("ExitCode = %d, want %d; output: %s", result.ExitCode, tt.wantExitCode, result.Output)
			}

			if tt.validate != nil {
				tt.validate(t, result)
			} else if result.Output != tt.wantOutput {
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
)

func Run(ctx context.Context) error {
	fmt.Println("No tools needed")
	return nil
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
)

func Run(ctx context.Context) error {
	<-ctx.Done()
	fmt.Println("graceful shutdown")
	return nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 1)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v", err)
	}

	// Process should exit cleanly after receiving SIGINT
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
)

func Run(ctx context.Context) error {
	// Ignore SIGINT
	signal.Ignore(os.Interrupt)
	time.Sleep(30 * time.Second)
	return nil
}
`

	result, err := ExecuteCode(ctx, nil, llmCode, 1)
	if err != nil {
		t.Fatalf("ExecuteCode() error: %v", err)
	}

	// Process should be killed with SIGKILL after grace period, exit code -1 on Linux
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
)

func Run(ctx context.Context) error {
	<-ctx.Done()
	fmt.Println("parent cancelled")
	return nil
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

	// Process should exit cleanly after parent context cancelled
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; output: %s", result.ExitCode, result.Output)
	}

	if !strings.Contains(result.Output, "parent cancelled") {
		t.Errorf("Output = %q, want to contain 'parent cancelled'", result.Output)
	}
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

	mainGo, err := GenerateMainGo(servers)
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

import "context"

func Run(ctx context.Context) error {
	var _ GetWeatherInput
	var _ GetWeatherOutput
	return nil
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "run.go"), []byte(runGo), 0644); err != nil {
		t.Fatalf("WriteFile(run.go) error: %v", err)
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy error: %v\n%s", err, out)
	}

	cmd = exec.Command("go", "build", ".")
	cmd.Dir = tempDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error: %v\n%s\n\nGenerated main.go:\n%s", err, out, mainGo)
	}
}
