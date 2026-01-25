package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("hello from generated code")
	return nil, nil
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

func Run(ctx context.Context) ([]mcp.Content, error) {
	this is not valid go
	return nil, nil
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

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	_ = bar // undefined
	return nil, nil
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, errors.New("intentional error from Run")
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

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("about to fatal exit")
	os.Exit(3)
	return nil, nil
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	time.Sleep(10 * time.Second)
	return nil, nil
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
				contentStr, ok := msg.Blocks[0].Content.(gai.Str)
				if !ok {
					t.Fatalf("expected Content to be gai.Str, got %T", msg.Blocks[0].Content)
				}
				output := string(contentStr)
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
			if len(msg.Blocks) < 1 {
				t.Fatalf("expected at least 1 block, got %d", len(msg.Blocks))
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

			contentStr, ok := block.Content.(gai.Str)
			if !ok {
				t.Fatalf("expected Content to be gai.Str, got %T", block.Content)
			}
			output := string(contentStr)
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	time.Sleep(30 * time.Second)
	return nil, nil
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

// blockSnapshot is a snapshot-friendly representation of gai.Block
type blockSnapshot struct {
	ModalityType string
	MimeType     string
	Content      string
}

func blocksToSnapshots(blocks []gai.Block) []blockSnapshot {
	result := make([]blockSnapshot, len(blocks))
	for i, b := range blocks {
		result[i] = blockSnapshot{
			ModalityType: b.ModalityType.String(),
			MimeType:     b.MimeType,
			Content:      b.Content.String(),
		}
	}
	return result
}

func TestContentToBlocks(t *testing.T) {
	tests := []struct {
		name    string
		content []mcp.Content
	}{
		{
			name:    "empty content",
			content: nil,
		},
		{
			name: "text content only",
			content: []mcp.Content{
				&mcp.TextContent{Text: "hello world"},
			},
		},
		{
			name: "image content",
			content: []mcp.Content{
				&mcp.ImageContent{Data: []byte("fake-image-data"), MIMEType: "image/png"},
			},
		},
		{
			name: "audio content",
			content: []mcp.Content{
				&mcp.AudioContent{Data: []byte("fake-audio-data"), MIMEType: "audio/wav"},
			},
		},
		{
			name: "PDF content returns image block",
			content: []mcp.Content{
				&mcp.ImageContent{Data: []byte("fake-pdf-data"), MIMEType: "application/pdf"},
			},
		},
		{
			name: "mixed content",
			content: []mcp.Content{
				&mcp.TextContent{Text: "description"},
				&mcp.ImageContent{Data: []byte("image"), MIMEType: "image/jpeg"},
				&mcp.AudioContent{Data: []byte("audio"), MIMEType: "audio/mp3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := contentToBlocks(tt.content)
			cupaloy.SnapshotT(t, blocksToSnapshots(blocks))
		})
	}
}

// messageSnapshot is a snapshot-friendly representation of gai.Message
type messageSnapshot struct {
	Role   string
	Blocks []msgBlockSnapshot
}

type msgBlockSnapshot struct {
	ID           string
	ModalityType string
	MimeType     string
	Content      string
}

func messageToSnapshot(msg gai.Message) messageSnapshot {
	blocks := make([]msgBlockSnapshot, len(msg.Blocks))
	for i, b := range msg.Blocks {
		blocks[i] = msgBlockSnapshot{
			ID:           b.ID,
			ModalityType: b.ModalityType.String(),
			MimeType:     b.MimeType,
			Content:      b.Content.String(),
		}
	}
	return messageSnapshot{
		Role:   msg.Role.String(),
		Blocks: blocks,
	}
}

func TestExecuteGoCodeCallback_MultimediaContent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multimedia content test in short mode")
	}

	tests := []struct {
		name string
		code string
	}{
		{
			name: "content only - no stdout",
			code: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return []mcp.Content{
		&mcp.TextContent{Text: "content text"},
	}, nil
}
`,
		},
		{
			name: "stdout and content - mixed",
			code: `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("stdout output")
	return []mcp.Content{
		&mcp.TextContent{Text: "content text"},
	}, nil
}
`,
		},
		{
			name: "empty result - no stdout no content",
			code: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`,
		},
		{
			name: "image content",
			code: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return []mcp.Content{
		&mcp.ImageContent{Data: []byte("fake-png"), MIMEType: "image/png"},
	}, nil
}
`,
		},
		{
			name: "audio content",
			code: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return []mcp.Content{
		&mcp.AudioContent{Data: []byte("fake-wav"), MIMEType: "audio/wav"},
	}, nil
}
`,
		},
		{
			name: "PDF content returns image block",
			code: `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return []mcp.Content{
		&mcp.ImageContent{Data: []byte("fake-pdf"), MIMEType: "application/pdf"},
	}, nil
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callback := &ExecuteGoCodeCallback{Servers: nil}

			input := ExecuteGoCodeInput{
				Code:             tt.code,
				ExecutionTimeout: 30,
			}

			inputJSON, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("failed to marshal input: %v", err)
			}

			msg, err := callback.Call(context.Background(), inputJSON, "test-id")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			cupaloy.SnapshotT(t, messageToSnapshot(msg))
		})
	}
}
