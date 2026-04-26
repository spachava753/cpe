package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/goyek/goyek/v2"
)

// Lint runs repository lint checks used by local development and CI.
// It executes `go tool golangci-lint run ./...` (honoring -lint-fix and -lint-verbose),
// checks for unreachable code with `go tool deadcode -test ./...`, and then enforces
// repo-specific architecture rules.
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
