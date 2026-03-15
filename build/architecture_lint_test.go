package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintImportBoundariesAt(t *testing.T) {
	t.Run("allows internal cmd imports of commands and version only", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/cmd/root.go", "package cmd\n\nimport (\n\t\"example.com/project/internal/commands\"\n\t\"example.com/project/internal/version\"\n)\n")
		writeTestFile(t, root, "internal/commands/root.go", "package commands\n")
		writeTestFile(t, root, "internal/version/version.go", "package version\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %v", issues)
		}
	})

	t.Run("rejects extra module-local internal cmd imports", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/cmd/root.go", "package cmd\n\nimport (\n\t\"example.com/project/internal/commands\"\n\t\"example.com/project/internal/config\"\n)\n")
		writeTestFile(t, root, "internal/commands/root.go", "package commands\n")
		writeTestFile(t, root, "internal/config/config.go", "package config\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %v", issues)
		}
		want := "internal/cmd/root.go"
		if got := issues[0]; got == "" || !contains(got, want) || !contains(got, "internal/config") {
			t.Fatalf("expected issue mentioning %q and internal/config, got %q", want, got)
		}
	})

	t.Run("rejects cobra and pflag inside internal commands", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/cmd/root.go", "package cmd\n")
		writeTestFile(t, root, "internal/commands/root.go", "package commands\n\nimport (\n\t\"github.com/spf13/cobra\"\n\t\"github.com/spf13/pflag\"\n)\n\nvar _ = cobra.Command{}\nvar _ *pflag.FlagSet\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 2 {
			t.Fatalf("expected 2 issues, got %v", issues)
		}
		if !contains(issues[0], "github.com/spf13/") && !contains(issues[1], "github.com/spf13/") {
			t.Fatalf("expected issues mentioning spf13 imports, got %v", issues)
		}
	})

	t.Run("rejects internal config importing internal mcp", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/cmd/root.go", "package cmd\n")
		writeTestFile(t, root, "internal/config/config.go", "package config\n\nimport \"example.com/project/internal/mcp\"\n\nvar _ mcp.MCPState\n")
		writeTestFile(t, root, "internal/mcp/state.go", "package mcp\n\ntype MCPState struct{}\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %v", issues)
		}
		if got := issues[0]; !contains(got, "internal/config/config.go") || !contains(got, "internal/mcp") {
			t.Fatalf("unexpected issue: %q", got)
		}
	})

	t.Run("rejects internal commands importing agent outside runtime orchestration files", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/commands/conversation.go", "package commands\n\nimport \"example.com/project/internal/agent\"\n\nvar _ = agent.ModelTypeResponses\n")
		writeTestFile(t, root, "internal/agent/doc.go", "package agent\n\nconst ModelTypeResponses = \"responses\"\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %v", issues)
		}
		if got := issues[0]; !contains(got, "internal/commands/conversation.go") || !contains(got, "internal/agent") {
			t.Fatalf("unexpected issue: %q", got)
		}
	})

	t.Run("allows internal commands runtime files to import agent", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/commands/root.go", "package commands\n\nimport \"example.com/project/internal/agent\"\n\nvar _ = agent.ModelTypeResponses\n")
		writeTestFile(t, root, "internal/commands/mcp.go", "package commands\n\nimport \"example.com/project/internal/agent\"\n\nvar _ = agent.ModelTypeResponses\n")
		writeTestFile(t, root, "internal/agent/doc.go", "package agent\n\nconst ModelTypeResponses = \"responses\"\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %v", issues)
		}
	})

	t.Run("rejects internal agent importing extracted input or prompt packages", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/agent/bad.go", "package agent\n\nimport (\n\t\"example.com/project/internal/input\"\n\t\"example.com/project/internal/prompt\"\n)\n\nvar _ = input.BuildUserBlocks\nvar _ = prompt.SystemPromptTemplate\n")
		writeTestFile(t, root, "internal/input/input.go", "package input\n\nvar BuildUserBlocks any\n")
		writeTestFile(t, root, "internal/prompt/template.go", "package prompt\n\nvar SystemPromptTemplate any\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 2 {
			t.Fatalf("expected 2 issues, got %v", issues)
		}
	})

	t.Run("recurses into nested package directories", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/commands/nested/helper.go", "package nested\n\nimport \"github.com/spf13/cobra\"\n\nvar _ = cobra.Command{}\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %v", issues)
		}
		if got := issues[0]; !contains(got, "internal/commands/nested/helper.go") || !contains(got, "github.com/spf13/cobra") {
			t.Fatalf("unexpected issue: %q", got)
		}
	})

	t.Run("does not exempt nested root or mcp files from agent import rule", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/project\n\ngo 1.25.0\n")
		writeTestFile(t, root, "internal/commands/nested/root.go", "package nested\n\nimport \"example.com/project/internal/agent\"\n\nvar _ = agent.ModelTypeResponses\n")
		writeTestFile(t, root, "internal/agent/doc.go", "package agent\n\nconst ModelTypeResponses = \"responses\"\n")

		issues := lintImportBoundariesAt(root)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %v", issues)
		}
		if got := issues[0]; !contains(got, "internal/commands/nested/root.go") || !contains(got, "internal/agent") {
			t.Fatalf("unexpected issue: %q", got)
		}
	})
}

func TestLintInternalCmdPackageAt(t *testing.T) {
	t.Run("allows init and Execute only", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "internal/cmd/root.go", "package cmd\n\nfunc Execute() {}\nfunc init() {}\n")

		issues := lintInternalCmdPackageAt(root)
		if len(issues) != 0 {
			t.Fatalf("expected no issues, got %v", issues)
		}
	})

	t.Run("rejects extra helper functions in internal cmd", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, root, "internal/cmd/root.go", "package cmd\n\nfunc Execute() {}\nfunc init() {}\nfunc helper() {}\n")

		issues := lintInternalCmdPackageAt(root)
		if len(issues) != 1 {
			t.Fatalf("expected 1 issue, got %v", issues)
		}
		if got := issues[0]; !contains(got, "function \"helper\"") {
			t.Fatalf("unexpected issue: %q", got)
		}
	})

}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
