package lintanalysis

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/go/analysis/passes/scannererr"
	"golang.org/x/tools/go/analysis/passes/sqlrowserr"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/lintfixture\n\ngo 1.26.6\n"
	for name, source := range files {
		if err := os.WriteFile(
			filepath.Join(
				dir,
				name,
			),
			[]byte(source),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunFixesTestVariants(t *testing.T) {
	const source = `package fixture

func Remove(values []int, i int) []int {
 return append(values[:i], values[i+1:]...)
}
`
	dir := fixture(
		t,
		map[string]string{
			"fixture.go": source,
			"fixture_test.go": `package fixture_test

import "example.com/lintfixture"

func ExampleRemove() {
 _ = fixture.Remove([]int{1, 2}, 0)
}
`,
		},
	)
	var output bytes.Buffer
	opts := Options{
		Output:  &output,
		Dir:     dir,
		Verbose: true,
	}
	analyzers := []*analysis.Analyzer{
		modernize.SlicesDeleteAnalyzer,
	}
	patterns := []string{
		"./...",
	}
	flags := flag.CommandLine
	if err := Run(
		t.Context(),
		analyzers,
		patterns,
		opts,
	); err == nil || !strings.Contains(
		output.String(),
		"Replace append with slices.Delete",
	) {
		t.Fatalf(
			"missing diagnostic: %v\n%s",
			err,
			&output,
		)
	}
	path := filepath.Join(
		dir,
		"fixture.go",
	)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != source {
		t.Fatal("check mode changed source")
	}
	output.Reset()
	opts.Fix = true
	if fixErr := Run(
		t.Context(),
		analyzers,
		patterns,
		opts,
	); fixErr != nil {
		t.Fatal(fixErr)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(fixed),
		"slices.Delete(values, i, i+1)",
	) || !strings.Contains(
		string(fixed),
		`import "slices"`,
	) {
		t.Fatalf(
			"fix not applied: %s",
			fixed,
		)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal("fix changed file permissions")
	}
	if !strings.Contains(
		output.String(),
		"slicesdelete@",
	) {
		t.Fatal("verbose output omitted analyzer execution")
	}
	if flag.CommandLine != flags {
		t.Fatal("runner changed global flag parsing")
	}
}

func TestRunUnfixableDiagnostics(t *testing.T) {
	dir := fixture(
		t,
		map[string]string{
			"fixture.go": `package fixture

import (
 "bufio"
 "database/sql"
 "io"
)

func Remove(values []int, i int) []int {
 return append(values[:i], values[i+1:]...)
}

func Scan(r io.Reader) {
 scanner := bufio.NewScanner(r)
 for scanner.Scan() {}
}

func Query(db *sql.DB) error {
 rows, err := db.Query("SELECT 1")
 if err != nil { return err }
 defer rows.Close()
 for rows.Next() {}
 return nil
}
`,
		},
	)
	var output bytes.Buffer
	err := Run(
		t.Context(),
		[]*analysis.Analyzer{
			scannererr.Analyzer,
			sqlrowserr.Analyzer,
			modernize.SlicesDeleteAnalyzer,
		},
		[]string{
			"./...",
		},
		Options{
			Output: &output,
			Dir:    dir,
			Fix:    true,
		},
	)
	if err == nil {
		t.Fatal("-fix hid unfixable diagnostics")
	}
	fixed, readErr := os.ReadFile(filepath.Join(
		dir,
		"fixture.go",
	))
	if readErr != nil || !strings.Contains(
		string(fixed),
		"slices.Delete",
	) {
		t.Fatal("fixable finding was not fixed before rechecking")
	}
	for _, want := range []string{
		"scanner.Err()",
		"rows.Err()",
	} {
		if !strings.Contains(
			output.String(),
			want,
		) {
			t.Fatalf(
				"missing %q diagnostic: %s",
				want,
				&output,
			)
		}
	}
}

func TestRunFailures(t *testing.T) {
	for _, test := range []struct {
		name          string
		source        string
		want          string
		patterns      []string
		analyzerError bool
	}{
		{
			name:   "package error",
			source: "package fixture\nvar value = undefined\n",
			patterns: []string{
				"./...",
			},
			want: "undefined",
		},
		{
			name:   "no matching packages",
			source: "package fixture\n",
			patterns: []string{
				"./empty/...",
			},
			want: "no packages matched",
		},
		{
			name:   "analyzer error",
			source: "package fixture\n",
			patterns: []string{
				"./...",
			},
			want:          "fixture analysis failed",
			analyzerError: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				dir := fixture(
					t,
					map[string]string{
						"fixture.go": test.source,
					},
				)
				if err := os.Mkdir(
					filepath.Join(
						dir,
						"empty",
					),
					0o700,
				); err != nil {
					t.Fatal(err)
				}
				analyzer := &analysis.Analyzer{
					Name: "fixture",
					Doc:  "fixture analysis",
					Run: func(*analysis.Pass) (any, error) {
						if test.analyzerError {
							return nil, errors.New("fixture analysis failed")
						}
						return nil, nil
					},
				}
				err := Run(
					t.Context(),
					[]*analysis.Analyzer{
						analyzer,
					},
					test.patterns,
					Options{
						Output: &bytes.Buffer{},
						Dir:    dir,
						Fix:    true,
					},
				)
				if err == nil || !strings.Contains(
					err.Error(),
					test.want,
				) {
					t.Fatalf(
						"Run error = %v, want %q",
						err,
						test.want,
					)
				}
				data, readErr := os.ReadFile(filepath.Join(
					dir,
					"fixture.go",
				))
				if readErr != nil || string(data) != test.source {
					t.Fatal("failed analysis changed source")
				}
			},
		)
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Run(
		ctx,
		nil,
		[]string{
			"./...",
		},
		Options{
			Output: &bytes.Buffer{},
		},
	)
	if !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf(
			"canceled run: %v",
			err,
		)
	}
}

func TestRunRejectsInvalidFixesBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name     string
		text     string
		want     string
		conflict bool
	}{
		{
			name:     "conflicting fixes",
			text:     "renamed",
			want:     "conflicting suggested edits",
			conflict: true,
		},
		{
			name: "invalid Go source",
			text: "!",
			want: "format suggested fixes",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				files := map[string]string{
					"a.go": "package fixture\nvar A = 1\n",
					"b.go": "package fixture\nvar B = 2\n",
				}
				dir := fixture(
					t,
					files,
				)
				analyzer := &analysis.Analyzer{
					Name: "fixture",
					Doc:  "fixture suggested fixes",
					Run: func(pass *analysis.Pass) (any, error) {
						for _, file := range pass.Files {
							report := func(text string) {
								pass.Report(analysis.Diagnostic{
									Pos:     file.Name.Pos(),
									Message: "rename package",
									SuggestedFixes: []analysis.SuggestedFix{
										{
											Message: "rename package",
											TextEdits: []analysis.TextEdit{
												{
													Pos:     file.Name.Pos(),
													End:     file.Name.End(),
													NewText: []byte(text),
												},
											},
										},
									},
								})
							}
							report(test.text)
							if test.conflict && strings.HasSuffix(
								pass.Fset.Position(file.Pos()).Filename,
								"b.go",
							) {
								report("other")
							}
						}
						return nil, nil
					},
				}
				err := Run(
					t.Context(),
					[]*analysis.Analyzer{
						analyzer,
					},
					[]string{
						"./...",
					},
					Options{
						Output: &bytes.Buffer{},
						Dir:    dir,
						Fix:    true,
					},
				)
				if err == nil || !strings.Contains(
					err.Error(),
					test.want,
				) {
					t.Fatalf(
						"fix error = %v, want %q",
						err,
						test.want,
					)
				}
				for name, source := range files {
					data, readErr := os.ReadFile(filepath.Join(
						dir,
						name,
					))
					if readErr != nil || string(data) != source {
						t.Fatalf(
							"invalid fixes changed %s",
							name,
						)
					}
				}
			},
		)
	}
}

func TestRunOptionalModernizers(t *testing.T) {
	dir := fixture(
		t,
		map[string]string{
			"fixture.go": `package fixture

import "fmt"

func Clone(values []int) []int {
 return append([]int{}, values...)
}

func Remove(values []int, i uint) []int {
 return append(values[:i], values[i+1:]...)
}

func Format(n int) []byte {
 return []byte(fmt.Sprintf("value:%d", n))
}
`,
			"fixture_test.go": `package fixture

import "testing"

func BenchmarkWork(b *testing.B) {
 b.ResetTimer()
 for range b.N {
  _ = Format(1)
 }
}
`,
		},
	)
	if err := Run(
		t.Context(),
		[]*analysis.Analyzer{
			modernize.AppendClippedAnalyzer,
			modernize.BLoopAnalyzer,
			modernize.FmtAppendfAnalyzer,
			modernize.SlicesDeleteAnalyzer,
		},
		[]string{
			"./...",
		},
		Options{
			Output: &bytes.Buffer{},
			Dir:    dir,
			Fix:    true,
		},
	); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"fixture.go": {
			"slices.Clone(values)",
			"slices.Delete(values, int(i), int(i+1))",
			"fmt.Appendf(nil,",
		},
		"fixture_test.go": {
			"for b.Loop()",
		},
	} {
		data, err := os.ReadFile(filepath.Join(
			dir,
			name,
		))
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range want {
			if !strings.Contains(
				string(data),
				text,
			) {
				t.Fatalf(
					"%s missing %q: %s",
					name,
					text,
					data,
				)
			}
		}
	}
}
