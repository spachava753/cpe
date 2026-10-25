package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/goyek/goyek/v2"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/go/analysis/passes/scannererr"
	"golang.org/x/tools/go/analysis/passes/sqlrowserr"

	"github.com/spachava753/cpe/build/internal/lintanalysis"
)

const goTool = "tool"

func runLintAnalyzers(a *goyek.A, patterns []string) {
	if err := lintanalysis.Run(
		a.Context(),
		[]*analysis.Analyzer{
			scannererr.Analyzer,
			sqlrowserr.Analyzer,
			modernize.AppendClippedAnalyzer,
			modernize.BLoopAnalyzer,
			modernize.FmtAppendfAnalyzer,
			modernize.SlicesDeleteAnalyzer,
		},
		patterns,
		lintanalysis.Options{
			Output:  os.Stderr,
			Fix:     *lintFix,
			Verbose: *lintVerbose,
		},
	); err != nil {
		a.Errorf(
			"additional analyzers: %v",
			err,
		)
	}
}

// Register the repository lint task used by local development and CI.
// It checks formatting, runs golangci-lint and modernize, reports additional
// public analyzer diagnostics, and checks for unreachable code with deadcode.
// The -lint-fix flag also applies fixes from the optional modernizers.
var _ = goyek.Define(goyek.Task{
	Name:  "lint",
	Usage: "Run golangci-lint and custom linters. Use -lint-fix to auto-fix, -lint-verbose for details",
	Action: func(a *goyek.A) {
		formatArgs := []string{
			goTool,
			"golangci-lint",
			"fmt",
		}
		if !*lintFix {
			formatArgs = append(
				formatArgs,
				"--diff",
			)
		}
		if *lintVerbose {
			formatArgs = append(
				formatArgs,
				"-v",
			)
		}
		formatArgs = append(
			formatArgs,
			"./...",
		)
		runLintCommand(
			a,
			"formatters",
			formatArgs...,
		)

		args := []string{
			goTool,
			"golangci-lint",
			"run",
		}
		if *lintFix {
			args = append(
				args,
				"--fix",
			)
		}
		if *lintVerbose {
			args = append(
				args,
				"-v",
			)
		}
		args = append(
			args,
			"./...",
		)

		runLintCommand(
			a,
			"golangci-lint",
			args...,
		)

		modernizePackages := modernizePackageArgs(a)
		modernizeArgs := []string{
			goTool,
			"modernize",
		}
		if *lintFix {
			modernizeArgs = append(
				modernizeArgs,
				"-fix",
			)
		}
		if *lintVerbose {
			modernizeArgs = append(
				modernizeArgs,
				"-v",
			)
		}
		modernizeArgs = append(
			modernizeArgs,
			modernizePackages...,
		)
		if len(modernizePackages) != 0 {
			runLintCommand(
				a,
				"modernize",
				modernizeArgs...,
			)
		}

		runLintAnalyzers(
			a,
			modernizePackages,
		)

		runLintCommand(
			a,
			"deadcode",
			goTool,
			"deadcode",
			"-test",
			"./...",
		)
	},
})

type listedPackage struct {
	ImportPath   string
	Dir          string
	GoFiles      []string
	TestGoFiles  []string
	XTestGoFiles []string
}

func modernizePackageArgs(a *goyek.A) []string {
	cmd := exec.CommandContext(
		a.Context(),
		"go",
		"list",
		"-json",
		"./...",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.Fatalf(
			"failed to list packages for modernize: %v\n%s",
			err,
			out,
		)
	}

	var args []string
	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			a.Fatalf(
				"failed to parse go list output for modernize: %v",
				err,
			)
		}
		if !packageHasGeneratedGoFiles(pkg) {
			args = append(
				args,
				pkg.ImportPath,
			)
		}
	}
	return args
}

// modernize cannot exclude individual generated files within a package, so skip
// packages containing generated files and leave those sources to their generator.
func packageHasGeneratedGoFiles(pkg listedPackage) bool {
	for _, name := range append(
		append(
			pkg.GoFiles,
			pkg.TestGoFiles...,
		),
		pkg.XTestGoFiles...,
	) {
		generated, err := isGeneratedGoFile(filepath.Join(
			pkg.Dir,
			name,
		))
		if err == nil && generated {
			return true
		}
	}
	return false
}

var generatedFileComment = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

func isGeneratedGoFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if len(data) > 4096 {
		data = data[:4096]
	}
	return generatedFileComment.Match(data), nil
}

func runLintCommand(a *goyek.A, name string, args ...string) {
	cmd := exec.CommandContext(
		a.Context(),
		"go",
		args...,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			a.Errorf(
				"%s issues found (exit code %d)",
				name,
				exitErr.ExitCode(),
			)
		} else {
			a.Fatalf(
				"failed to run %s: %v",
				name,
				err,
			)
		}
	}
}
