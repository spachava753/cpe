package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	for _, test := range []struct {
		name, source, testSource, symbol string
		fixable                          bool
	}{
		{name: "parameter captures reference", source: `const Value = 1; func f(value int) int { return Value }`, symbol: "Value"},
		{name: "local captures reference", source: `const Value = 1; func f() int { value := 2; return Value + value }`, symbol: "Value"},
		{name: "closure captures reference", source: `const Value = 1; func f(value int) func() int { return func() int { return Value } }`, symbol: "Value"},
		{name: "type parameter captures reference", source: `type Value int; func f[value any]() Value { return 1 }`, symbol: "Value"},
		{name: "same package test local", source: `const Value = 1`, testSource: `func f(value int) int { return Value }`, symbol: "Value"},
		{name: "same package test declaration", source: `const Value = 1`, testSource: `const value = 2`, symbol: "Value"},
		{name: "embedded field collision", source: `type Value int; type holder struct { Value; value int }; var _ = holder{Value:1,value:2}`, symbol: "Value"},
		{name: "embedded field and method", source: `type Value int; type holder struct { Value }; func (holder) value() {}; var _ = holder{}.value`, symbol: "Value"},
		{name: "embedded field hides promoted field", source: `type Value struct { value int }; type holder struct { Value }; var _ = holder{}.value`, symbol: "Value"},
		{name: "embedded field hides promoted method", source: `type Value struct{}; func (Value) value() {}; type holder struct { Value }; var _ = holder{}.value`, symbol: "Value"},
		{name: "embedding in same package test", source: `type Value int`, testSource: `type holder struct { Value; value int }; var _ = holder{Value:1,value:2}`, symbol: "Value"},
		{name: "embedded type requires manual review", source: `type Value int; type holder struct { Value }; var _ = holder{Value:1}.Value`, symbol: "Value"},
		{name: "equal depth promoted collision", source: `type Value int; type a struct{Value}; type b struct{value int}; type holder struct{a;b}; func result() int { h:=holder{a:a{Value:1},b:b{value:2}}; return h.value }`, symbol: "Value"},
		{name: "shallower promoted field captures selection", source: `type Value int; type a struct{Value}; type b struct{value int}; type c struct{b}; type holder struct{a;c}; func result() int { h:=holder{a:a{Value:1},c:c{b:b{value:2}}}; return h.value }`, symbol: "Value"},
		{name: "predeclared name in use", source: `const True = false; var flag = true; var _ = True`, symbol: "True"},
		{name: "keyword", source: `const Type = 1`, symbol: "Type"},
		{name: "local declared after reference", source: `const Value = 1; func f() int { value := Value; return value }`, symbol: "Value", fixable: true},
		{name: "unrelated local", source: `const Value = 1; func f(value int) int { return value }; var _ = Value`, symbol: "Value", fixable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeAnalyzerTestFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.26.4\n")
			source := "package source\n" + test.source + "\n"
			writeAnalyzerTestFile(t, dir, "source.go", source)
			if test.testSource != "" {
				writeAnalyzerTestFile(t, dir, "source_test.go", "package source\n"+test.testSource+"\n")
			}
			findings, err := findUnnecessaryExports(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 || findings[0].key.name != test.symbol || findings[0].fixable != test.fixable {
				t.Fatalf("findings = %+v, want %s fixable=%t", findings, test.symbol, test.fixable)
			}
			if !test.fixable {
				var report strings.Builder
				if err := runUnnecessaryExportAnalyzer(t.Context(), dir, true, &report); err == nil {
					t.Fatal("unsafe rename reported success")
				}
				got, err := os.ReadFile(filepath.Join(dir, "source.go"))
				if err != nil || string(got) != source {
					t.Fatalf("unsafe rename modified source: %s, %v", got, err)
				}
			}
		})
	}
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
	server *HTTPServer
}

var _ = holder{server: new(HTTPServer)}.server
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
	server *httpServer
}

var _ = holder{server: new(httpServer)}.server
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
