package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/goyek/goyek/v2"
)

// Lint runs repository lint checks used by local development and CI.
// It executes `go tool golangci-lint run ./...`, runs the modernize analyzer
// (honoring -lint-fix and -lint-verbose where supported), checks for unreachable
// code with `go tool deadcode -test ./...`, and then enforces repo-specific
// architecture rules.
var Lint = goyek.Define(goyek.Task{
	Name:  "lint",
	Usage: "Run golangci-lint and custom linters. Use -lint-fix to auto-fix, -lint-verbose for details",
	Action: func(a *goyek.A) {
		args := []string{"tool", "golangci-lint", "run"}
		if *lintFix {
			args = append(args, "--fix")
		}
		if *lintVerbose {
			args = append(args, "-v")
		}
		args = append(args, "./...")

		runLintCommand(a, "golangci-lint", args...)

		modernizeArgs := []string{"tool", "modernize"}
		if *lintFix {
			modernizeArgs = append(modernizeArgs, "-fix")
		}
		if *lintVerbose {
			modernizeArgs = append(modernizeArgs, "-v")
		}
		modernizeArgs = append(modernizeArgs, modernizePackageArgs(a)...)
		runLintCommand(a, "modernize", modernizeArgs...)

		runLintCommand(a, "deadcode", "tool", "deadcode", "-test", "./...")

		issues := append([]string{}, lintInternalCmdPackageAt(".")...)
		issues = append(issues, lintImportBoundariesAt(".")...)
		if len(issues) > 0 {
			for _, issue := range issues {
				fmt.Println(issue)
			}
			a.Errorf("found %d architecture lint issue(s)", len(issues))
		}
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
	cmd := exec.CommandContext(a.Context(), "go", "list", "-json", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.Fatalf("failed to list packages for modernize: %v\n%s", err, out)
	}

	var args []string
	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			a.Fatalf("failed to parse go list output for modernize: %v", err)
		}
		if !packageHasGeneratedGoFiles(pkg) {
			args = append(args, pkg.ImportPath)
		}
	}
	return args
}

// modernize cannot exclude individual generated files within a package, so skip
// packages containing generated files and leave those sources to their generator.
func packageHasGeneratedGoFiles(pkg listedPackage) bool {
	for _, name := range append(append(pkg.GoFiles, pkg.TestGoFiles...), pkg.XTestGoFiles...) {
		generated, err := isGeneratedGoFile(filepath.Join(pkg.Dir, name))
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
	cmd := exec.CommandContext(a.Context(), "go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			a.Errorf("%s issues found (exit code %d)", name, exitErr.ExitCode())
		} else {
			a.Fatalf("failed to run %s: %v", name, err)
		}
	}
}
