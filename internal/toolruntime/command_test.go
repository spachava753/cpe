package toolruntime

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/dyson"
)

func TestCommandRunner(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, test := range []struct {
		name      string
		command   string
		want      string
		input     []byte
		timeout   time.Duration
		code      int
		cancelled bool
	}{
		{
			name:    "exit and separate stderr",
			command: "printf hello; printf problem >&2; exit 7",
			code:    7,
			want:    "hello",
		},
		{
			name:    "stdin is closed",
			command: "if read line; then exit 1; fi; printf closed",
			want:    "closed",
		},
		{
			name:    "large stdin",
			command: "wc -c",
			input: []byte(strings.Repeat(
				"x",
				9<<20,
			)),
			want: "9437184",
		},
		{
			name:    "complete capture",
			command: "printf '%200000s' x; printf final-summary",
			want: strings.Repeat(
				" ",
				199999,
			) + "xfinal-summary",
		},
		{
			name:      "cancel sleeping child",
			command:   "sleep 100",
			timeout:   30 * time.Millisecond,
			cancelled: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				ctx := t.Context()
				if test.timeout != 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(
						ctx,
						test.timeout,
					)
					defer cancel()
				}
				stdin := dyson.StreamDiscard
				if test.input != nil {
					stdin = dyson.StreamPipe
				}
				runner := localCommandRunner{}
				result, err := runner.RunCommand(
					ctx,
					dyson.Command{
						Args: []string{
							bash,
							"--noprofile",
							"--norc",
							"-c",
							test.command,
						},
						Dir:    t.TempDir(),
						Stdin:  stdin,
						Input:  test.input,
						Stdout: dyson.StreamPipe,
						Stderr: dyson.StreamPipe,
					},
				)
				if test.cancelled {
					if !errors.Is(
						err,
						context.DeadlineExceeded,
					) {
						t.Fatalf(
							"cancellation: %v",
							err,
						)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if result.ReturnCode != test.code || !strings.Contains(
					string(result.Stdout),
					test.want,
				) {
					t.Fatalf(
						"result code=%d, bytes=%d",
						result.ReturnCode,
						len(result.Stdout),
					)
				}
				if test.name == "exit and separate stderr" && string(result.Stderr) != "problem" {
					t.Fatal("stderr was not captured separately")
				}
			},
		)
	}
}
