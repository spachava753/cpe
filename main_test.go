package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Exercise the executable so regressions in Fang's production error handler
// cannot be hidden by command tests that only inspect returned errors.
func TestCommandErrors(t *testing.T) {
	binary := filepath.Join(
		t.TempDir(),
		"cpe.exe",
	)
	build := exec.CommandContext(
		t.Context(),
		"go",
		"build",
		"-o",
		binary,
		".",
	)
	build.Env = append(
		os.Environ(),
		"GOPROXY=off",
		"GOSUMDB=off",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf(
			"build executable: %v\n%s",
			err,
			output,
		)
	}
	for _, test := range []struct {
		name       string
		args       []string
		filename   string
		forceColor bool
		want       string
		wantHelp   bool
	}{
		{
			name: "missing skill keeps ordinary quotes readable",
			args: []string{
				"skills",
				"rm",
				"terminal-control-demo",
			},
			want: `"terminal-control-demo" not found: file does not exist`,
		},
		{
			name: "usage errors keep readable command quotes and help",
			args: []string{
				"missing-command",
			},
			want:     `"missing-command" for "cpe"`,
			wantHelp: true,
		},
		{
			name: "error paths keep ordinary quotes and backslashes",
			args: []string{
				"skills",
				"rm",
				"alpha",
			},
			filename: `quoted "C:\work"`,
			want:     `quoted "C:\work" is not a directory`,
		},
		{
			name: "unsafe error controls are removed before trusted styling",
			args: []string{
				"skills",
				"rm",
				"alpha",
			},
			filename:   "quoted \"C:\\work\" \x1b[31mred\x1b[0m \u009b31mc1red\u009b0m \x1b]8;;link-target\x1b\\link\x1b]8;;\x1b\\ \x1b]52;c;YXR0YWNr\aok\b\u009d52;c;payload\aEND",
			forceColor: true,
			want:       `quoted "C:\work" red c1red link okEND is not a directory`,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.filename != "" && runtime.GOOS == "windows" {
					t.Skip("fixture filename is not valid on Windows")
				}
				home := t.TempDir()
				args := test.args
				if test.filename != "" {
					filename := filepath.Join(
						home,
						test.filename,
					)
					if err := os.WriteFile(
						filename,
						nil,
						0o600,
					); err != nil {
						t.Fatal(err)
					}
					args = append(
						args,
						"--dir",
						filename,
					)
				}
				var output, diagnostics bytes.Buffer
				command := exec.CommandContext(
					t.Context(),
					binary,
					args...,
				)
				command.Dir = home
				command.Env = append(
					os.Environ(),
					"HOME="+home,
					"USERPROFILE="+home,
					"NO_COLOR=1",
					"CLICOLOR_FORCE=0",
				)
				if test.forceColor {
					command.Env = append(
						command.Env,
						"NO_COLOR=",
						"CLICOLOR_FORCE=1",
					)
				}
				command.Stdout = &output
				command.Stderr = &diagnostics
				err := command.Run()
				var exit *exec.ExitError
				if !errors.As(
					err,
					&exit,
				) || exit.ExitCode() != 1 {
					t.Fatalf(
						"command error = %v, want exit status 1",
						err,
					)
				}
				plain := ansi.Strip(diagnostics.String())
				// Fang wraps long diagnostics; inspect readable text independently of
				// terminal width while checking controls in the original output below.
				readable := strings.Join(
					strings.Fields(plain),
					" ",
				)
				if output.Len() != 0 || !strings.Contains(
					readable,
					test.want,
				) {
					t.Fatalf(
						"stdout = %q, stderr = %q; want %q on stderr",
						&output,
						&diagnostics,
						test.want,
					)
				}
				if test.wantHelp && !strings.Contains(
					readable,
					"Try --help for usage.",
				) {
					t.Fatalf(
						"usage hint missing from %q",
						plain,
					)
				}
				for _, character := range plain {
					if character != '\n' && character != '\t' && unicode.IsControl(character) {
						t.Fatalf(
							"untrusted control in error output: %q",
							plain,
						)
					}
				}
				for _, unsafe := range []string{
					`\"`,
					`\x1b`,
					`\u009`,
					"YXR0YWNr",
					"payload",
				} {
					if strings.Contains(
						diagnostics.String(),
						unsafe,
					) {
						t.Fatalf(
							"error contains escaped or unsafe text %q: %q",
							unsafe,
							&diagnostics,
						)
					}
				}
				if test.forceColor && !strings.Contains(
					diagnostics.String(),
					"\x1b[",
				) {
					t.Fatal("trusted error styling was lost")
				}
			},
		)
	}
}
