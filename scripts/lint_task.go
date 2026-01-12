package main

import (
	"os"
	"os/exec"

	"github.com/goyek/goyek/v2"
)

// Lint runs golangci-lint on the codebase
var Lint = goyek.Define(goyek.Task{
	Name:  "lint",
	Usage: "Run golangci-lint. Use -lint-fix to auto-fix, -lint-verbose for details",
	Action: func(a *goyek.A) {
		args := []string{"tool", "golangci-lint", "run"}

		if GetLintFix() {
			args = append(args, "--fix")
		}

		if GetLintVerbose() {
			args = append(args, "-v")
		}

		args = append(args, "./...")

		cmd := exec.Command("go", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				// golangci-lint returns exit code 1 when issues found
				a.Errorf("Linting issues found (exit code %d)", exitErr.ExitCode())
			} else {
				a.Fatalf("Failed to run golangci-lint: %v", err)
			}
		}
	},
})
