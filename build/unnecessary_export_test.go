package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestUnexportedName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "Handler", want: "handler"},
		{name: "HTTPServer", want: "httpServer"},
		{name: "URL", want: "url"},
		{name: "MCPCodeDesc", want: "mcpCodeDesc"},
		{name: "OAuthTransport", want: "oauthTransport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := unexportedName(tt.name); got != tt.want {
				t.Fatalf("unexportedName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestFindUnnecessaryExports(t *testing.T) {
	t.Run("ignores declarations used by another package", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeAnalyzerTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.26.4\n")
		writeAnalyzerTestFile(t, dir, "source/source.go", `package source

// UsedOutside is consumed by another package.
type UsedOutside struct{}

// InternalOnly is private implementation detail.
type InternalOnly struct{}

// BuildInternal constructs an internal value.
func BuildInternal() InternalOnly { return InternalOnly{} }

// ExportedConstant is only used here.
const ExportedConstant = 1

var _ = ExportedConstant
`)
		writeAnalyzerTestFile(t, dir, "consumer/consumer.go", `package consumer

import "example.com/test/source"

var _ source.UsedOutside
`)

		findings, err := findUnnecessaryExports(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, finding := range findings {
			names = append(names, finding.key.name)
		}
		slices.Sort(names)
		want := []string{"BuildInternal", "ExportedConstant", "InternalOnly"}
		if !slices.Equal(names, want) {
			t.Fatalf("finding names = %q, want %q", names, want)
		}
	})
	t.Run("reports", func(t *testing.T) {
		t.Run("import collision without fix", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeAnalyzerTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.26.4\n")
			writeAnalyzerTestFile(t, dir, "source.go", `package source

type Fmt struct{}

var _ Fmt
`)
			writeAnalyzerTestFile(t, dir, "source_internal_test.go", `package source

import "fmt"

var _ = fmt.Sprint
`)

			findings, err := findUnnecessaryExports(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %#v, want one", findings)
			}
			if findings[0].fixable {
				t.Fatalf("finding = %#v, want non-fixable import collision", findings[0])
			}
			if findings[0].reason != "fmt is imported by a file in the package" {
				t.Fatalf("reason = %q, want import collision", findings[0].reason)
			}
		})
		t.Run("rename collision without fix", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeAnalyzerTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.26.4\n")
			writeAnalyzerTestFile(t, dir, "source.go", `package source

func Helper() {}
func helper() {}

var _ = Helper
var _ = helper
`)

			findings, err := findUnnecessaryExports(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %#v, want one", findings)
			}
			if findings[0].fixable {
				t.Fatalf("finding = %#v, want non-fixable collision", findings[0])
			}
			if findings[0].reason != "helper already exists" {
				t.Fatalf("reason = %q, want %q", findings[0].reason, "helper already exists")
			}
		})
	})
}

func TestRunUnnecessaryExportAnalyzerFixesDeclarationsReferencesAndDocs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeAnalyzerTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.26.4\n")
	writeAnalyzerTestFile(t, dir, "source.go", `package source

// HTTPServer is used only in this package.
type HTTPServer struct{}

// NewHTTPServer constructs an HTTPServer.
func NewHTTPServer() HTTPServer { return HTTPServer{} }

type holder struct {
	*HTTPServer
}

var _ = holder{HTTPServer: new(HTTPServer)}.HTTPServer
var _ = NewHTTPServer()
`)

	if err := runUnnecessaryExportAnalyzer(context.Background(), dir, true, os.Stderr); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "source.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := `package source

// httpServer is used only in this package.
type httpServer struct{}

// newHTTPServer constructs an httpServer.
func newHTTPServer() httpServer { return httpServer{} }

type holder struct {
	*httpServer
}

var _ = holder{httpServer: new(httpServer)}.httpServer
var _ = newHTTPServer()
`
	if string(got) != want {
		t.Fatalf("fixed source:\n%s\nwant:\n%s", got, want)
	}
}

func writeAnalyzerTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
