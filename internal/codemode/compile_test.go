package codemode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpcpe "github.com/spachava753/cpe/internal/mcp"
)

func TestGenerateMainGo_Compiles(t *testing.T) {
	testCases := []struct {
		name    string
		servers []ServerToolsInfo
	}{
		{
			name:    "empty",
			servers: []ServerToolsInfo{},
		},
		{
			name: "stdio_with_tools",
			servers: []ServerToolsInfo{
				{
					ServerName: "editor",
					Config:     mcpcpe.ServerConfig{Type: "stdio", Command: "editor-mcp"},
					Tools: []*mcp.Tool{
						{
							Name:         "read_file",
							InputSchema:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
							OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"content": map[string]any{"type": "string"}}},
						},
					},
				},
			},
		},
		{
			name: "http_with_headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "api",
					Config:     mcpcpe.ServerConfig{Type: "http", URL: "https://api.example.com", Headers: map[string]string{"Authorization": "Bearer token"}},
					Tools: []*mcp.Tool{
						{
							Name:         "fetch",
							InputSchema:  map[string]any{},
							OutputSchema: nil,
						},
					},
				},
			},
		},
		{
			name: "server_no_tools",
			servers: []ServerToolsInfo{
				{
					ServerName: "empty",
					Config:     mcpcpe.ServerConfig{Type: "stdio", Command: "empty-mcp"},
					Tools:      []*mcp.Tool{},
				},
			},
		},
		{
			name: "multiple_servers_different_headers",
			servers: []ServerToolsInfo{
				{
					ServerName: "api-one",
					Config:     mcpcpe.ServerConfig{Type: "http", URL: "https://one.example.com", Headers: map[string]string{"X-Key": "one"}},
					Tools: []*mcp.Tool{
						{Name: "one", InputSchema: map[string]any{}, OutputSchema: nil},
					},
				},
				{
					ServerName: "api-two",
					Config:     mcpcpe.ServerConfig{Type: "http", URL: "https://two.example.com", Headers: map[string]string{"X-Key": "two"}},
					Tools: []*mcp.Tool{
						{Name: "two", InputSchema: map[string]any{}, OutputSchema: nil},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mainGo, err := GenerateMainGo(tc.servers, "/tmp/content.json")
			if err != nil {
				t.Fatalf("GenerateMainGo() error: %v", err)
			}

			// Snapshot the generated code
			cupaloy.SnapshotT(t, mainGo)

			// Verify the generated code compiles
			tmpDir, err := os.MkdirTemp("", "cpe-compile-test-*")
			if err != nil {
				t.Fatalf("MkdirTemp() error: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			goMod := `module test
go 1.24
require github.com/modelcontextprotocol/go-sdk v1.1.0
`
			if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
				t.Fatalf("WriteFile(go.mod) error: %v", err)
			}

			if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644); err != nil {
				t.Fatalf("WriteFile(main.go) error: %v", err)
			}

			runGo := `package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context) ([]mcp.Content, error) {
	return nil, nil
}
`
			if err := os.WriteFile(filepath.Join(tmpDir, "run.go"), []byte(runGo), 0644); err != nil {
				t.Fatalf("WriteFile(run.go) error: %v", err)
			}

			cmd := exec.CommandContext(context.Background(), "go", "mod", "tidy")
			cmd.Dir = tmpDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy error: %v\n%s", err, out)
			}

			cmd = exec.CommandContext(context.Background(), "go", "build", ".")
			cmd.Dir = tmpDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build error: %v\n%s\n\nGenerated main.go:\n%s", err, out, mainGo)
			}
		})
	}
}
