package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spachava753/gai"
)

func TestExecuteGoCodeCallback_Call(t *testing.T) {
	tests := []struct {
		name           string
		input          ExecuteGoCodeInput
		wantErr        bool
		wantFatalErr   bool
		wantOutputSub  string // substring expected in output
		wantErrContain string // substring expected in error message
	}{
		{
			name: "successful execution",
			input: ExecuteGoCodeInput{
				Code: `package main

import (
	"context"
	"fmt"
)

func Run(ctx context.Context) error {
	fmt.Println("hello from generated code")
	return nil
}
`,
				ExecutionTimeout: 30,
			},
			wantOutputSub: "hello from generated code",
		},
		{
			name: "compilation error - syntax",
			input: ExecuteGoCodeInput{
				Code: `package main

func Run(ctx context.Context) error {
	this is not valid go
	return nil
}
`,
				ExecutionTimeout: 30,
			},
			wantOutputSub: "syntax error",
		},
				{
			name: "compilation error - undefined variable",
			input: ExecuteGoCodeInput{
				Code: `package main

import "context"

func Run(ctx context.Context) error {
	_ = bar // undefined
	return nil
}
`,
				ExecutionTimeout: 30,
			},
			wantOutputSub: "undefined: bar",
		},
		{
			name: "Run returns error - exit code 1",
			input: ExecuteGoCodeInput{
				Code: `package main

import (
	"context"
	"errors"
)

func Run(ctx context.Context) error {
	return errors.New("intentional error from Run")
}
`,
				ExecutionTimeout: 30,
			},
			wantOutputSub: "intentional error from Run",
		},
		{
			name: "panic - exit code 2",
			input: ExecuteGoCodeInput{
				Code: `package main

import "context"

func Run(ctx context.Context) error {
	panic("intentional panic")
}
`,
				ExecutionTimeout: 30,
			},
			wantOutputSub: "intentional panic",
		},
		{
			name: "fatal exit - exit code 3",
			input: ExecuteGoCodeInput{
				Code: `package main

import (
	"context"
	"fmt"
	"os"
)

func Run(ctx context.Context) error {
	fmt.Println("about to fatal exit")
	os.Exit(3)
	return nil
}
`,
				ExecutionTimeout: 30,
			},
			wantErr:        true,
			wantFatalErr:   true,
			wantErrContain: "about to fatal exit",
		},
		{
			name: "timeout exceeded",
			input: ExecuteGoCodeInput{
				Code: `package main

import (
	"context"
	"time"
)

func Run(ctx context.Context) error {
	time.Sleep(10 * time.Second)
	return nil
}
`,
				ExecutionTimeout: 1,
			},
			// Timeout causes SIGINT then SIGKILL, resulting in recoverable error
			wantOutputSub: "", // output may be empty or contain partial output
		},
		{
			name: "invalid JSON parameters",
			input: ExecuteGoCodeInput{
				Code:             "", // Will test with raw invalid JSON
				ExecutionTimeout: 0,
			},
			// Special case: we'll test this separately
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "invalid JSON parameters" {
				// Test invalid JSON separately
				callback := &ExecuteGoCodeCallback{Servers: nil}
				msg, err := callback.Call(context.Background(), []byte(`{invalid json}`), "test-id")
				if err != nil {
					t.Fatalf("expected no error for invalid JSON (should return tool result), got: %v", err)
				}
				if msg.Role != gai.ToolResult {
					t.Errorf("expected ToolResult role, got %v", msg.Role)
				}
				output := string(msg.Blocks[0].Content.(gai.Str))
				if !strings.Contains(output, "Error parsing parameters") {
					t.Errorf("expected error parsing message, got: %s", output)
				}
				return
			}

			// Skip timeout test in short mode as it takes time
			if tt.name == "timeout exceeded" && testing.Short() {
				t.Skip("skipping timeout test in short mode")
			}

			callback := &ExecuteGoCodeCallback{Servers: nil}

			inputJSON, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("failed to marshal input: %v", err)
			}

			msg, err := callback.Call(context.Background(), inputJSON, "test-tool-call-id")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.wantFatalErr {
					var fatalErr FatalExecutionError
					if !errors.As(err, &fatalErr) {
						t.Errorf("expected FatalExecutionError, got %T: %v", err, err)
					}
					if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
						t.Errorf("expected error to contain %q, got: %v", tt.wantErrContain, err)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify message structure
			if msg.Role != gai.ToolResult {
				t.Errorf("expected ToolResult role, got %v", msg.Role)
			}
			if len(msg.Blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(msg.Blocks))
			}
			block := msg.Blocks[0]
			if block.ID != "test-tool-call-id" {
				t.Errorf("expected block ID 'test-tool-call-id', got %q", block.ID)
			}
			if block.BlockType != gai.Content {
				t.Errorf("expected Content block type, got %v", block.BlockType)
			}
			if block.ModalityType != gai.Text {
				t.Errorf("expected Text modality, got %v", block.ModalityType)
			}

			output := string(block.Content.(gai.Str))
			if tt.wantOutputSub != "" && !strings.Contains(output, tt.wantOutputSub) {
				t.Errorf("expected output to contain %q, got: %s", tt.wantOutputSub, output)
			}
		})
	}
}

func TestExecuteGoCodeCallback_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	callback := &ExecuteGoCodeCallback{Servers: nil}

	input := ExecuteGoCodeInput{
		Code: `package main

import (
	"context"
	"time"
)

func Run(ctx context.Context) error {
	time.Sleep(30 * time.Second)
	return nil
}
`,
		ExecutionTimeout: 60,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a short delay
	go func() {
		cancel()
	}()

	// The call should return relatively quickly due to context cancellation
	msg, err := callback.Call(ctx, inputJSON, "test-id")

	// Context cancellation during go build/mod tidy could cause infrastructure error
	// or the program could be killed resulting in recoverable error
	if err != nil {
		// Infrastructure error is acceptable for context cancellation
		return
	}

	// If no error, should have a tool result
	if msg.Role != gai.ToolResult {
		t.Errorf("expected ToolResult role, got %v", msg.Role)
	}
}
