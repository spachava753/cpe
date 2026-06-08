package codemode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_correctFileImports(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) (dir string, filename string)
		wantDiff []string
		wantErr  bool
	}{
		{
			name: "adds missing imports for generated Run function",
			setup: func(t *testing.T) (string, string) {
				t.Helper()

				dir := t.TempDir()
				writeGeneratedCodeModule(t, dir)
				writeFile(t, filepath.Join(dir, "run.go"), `package main

func Run(ctx context.Context) ([]mcp.Content, error) {
	fmt.Println("hello")
	return nil, nil
}
`)

				return dir, "run.go"
			},
			wantDiff: []string{
				"+ context",
				"+ fmt",
				"+ github.com/modelcontextprotocol/go-sdk/mcp",
			},
		},
		{
			name: "removes unused import from generated Run function",
			setup: func(t *testing.T) (string, string) {
				t.Helper()

				dir := t.TempDir()
				writeGeneratedCodeModule(t, dir)
				writeFile(t, filepath.Join(dir, "run.go"), `package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`)

				return dir, "run.go"
			},
			wantDiff: []string{
				"- fmt",
			},
		},
		{
			name: "returns error for missing generated file",
			setup: func(t *testing.T) (string, string) {
				t.Helper()

				dir := t.TempDir()
				writeGeneratedCodeModule(t, dir)

				return dir, "run.go"
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, filename := tt.setup(t)

			got, gotErr := correctFileImports(dir, filename)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("correctFileImports() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("correctFileImports() succeeded unexpectedly")
			}
			for _, want := range tt.wantDiff {
				if !strings.Contains(got, want) {
					t.Errorf("correctFileImports() = %q, want diff containing %q", got, want)
				}
			}
		})
	}
}

func Test_correctFileImportsInvalidSyntaxIsRecoverable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeGeneratedCodeModule(t, dir)
	writeFile(t, filepath.Join(dir, "run.go"), `package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	this is not valid Go syntax
	return nil, nil
}
`)

	_, err := correctFileImports(dir, "run.go")
	if err == nil {
		t.Fatal("correctFileImports() succeeded unexpectedly")
	}
	if _, ok := errors.AsType[RecoverableError](err); !ok {
		t.Fatalf("correctFileImports() error = %T %[1]v, want RecoverableError", err)
	}
	if !strings.Contains(err.Error(), "expected ';', found is") {
		t.Fatalf("correctFileImports() error = %q, want syntax detail", err.Error())
	}
}

func TestExecuteGoCodeCallInvalidSyntaxReturnsToolResult(t *testing.T) {
	t.Parallel()

	callback := &ExecuteGoCodeCallback{MaxTimeout: 5}
	msg, err := callback.Call(context.Background(), map[string]any{
		"code": `package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	this is not valid Go syntax
	return nil, nil
}
`,
		"executionTimeout": 1,
	})
	if err != nil {
		t.Fatalf("Call() error = %v, want nil", err)
	}
	if len(msg.Blocks) != 1 || msg.Blocks[0].Content == nil {
		t.Fatalf("Call() returned %#v, want one text block", msg.Blocks)
	}
	got := msg.Blocks[0].Content.String()
	if !strings.Contains(got, "recoverable execution error") {
		t.Fatalf("Call() tool result = %q, want recoverable error", got)
	}
	if !strings.Contains(got, "expected ';', found is") {
		t.Fatalf("Call() tool result = %q, want syntax detail", got)
	}
}

func writeGeneratedCodeModule(t *testing.T, dir string) {
	t.Helper()

	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/test

go 1.26

require github.com/modelcontextprotocol/go-sdk v1.6.1
`)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
