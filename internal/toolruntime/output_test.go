package toolruntime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/spachava753/dyson"
)

func TestScriptOutput(t *testing.T) {
	long := strings.Repeat(
		"x",
		maxTextBytes+1,
	) + "final summary"
	for _, test := range []struct {
		name        string
		code        string
		stdout      string
		stderr      string
		full        string
		diagnostic  string
		exit        int
		cancel      bool
		saveFailure bool
		failed      bool
		completes   bool
	}{
		{
			name: "prints form one text block",
			code: "print('one')\nprint('two')\nprint('three')",
			full: "one\ntwo\nthree\n",
		},
		{
			name:      "long printed text",
			code:      "print(" + strconv.Quote(long) + ")",
			full:      long + "\n",
			completes: true,
		},
		{
			name:   "full command streams can be filtered before printing",
			code:   "load('subprocess.star', 'subprocess')\nr = subprocess.run(['build'], capture_output=True)\nprint(len(r.stdout))\nprint(r.stdout.decode()[-13:])\nprint(r.stderr.decode()[-7:])\nprint(r.returncode)",
			stdout: long,
			stderr: long + " stderr",
			exit:   7,
			full:   strconv.Itoa(len(long)) + "\nfinal summary\n stderr\n7\n",
		},
		{
			name:   "script chooses not to emit command streams",
			code:   "load('subprocess.star', 'subprocess')\nsubprocess.run(['build'], capture_output=True)\nprint('done')",
			stdout: long,
			stderr: long + " stderr",
			full:   "done\n",
		},
		{
			name:   "printed command streams form one overflow value",
			code:   "load('subprocess.star', 'subprocess')\nr = subprocess.run(['build'], capture_output=True)\nprint(r.stdout.decode())\nprint(r.stderr.decode())\nprint(r.returncode)",
			stdout: long,
			stderr: long + " stderr",
			exit:   7,
			full:   long + "\n" + long + " stderr\n7\n",
		},
		{
			name:       "emitted text survives cancellation",
			code:       "print(" + strconv.Quote(long) + ")\nload('subprocess.star', 'subprocess')\nsubprocess.run(['wait'])",
			cancel:     true,
			failed:     true,
			full:       long + "\n",
			diagnostic: "cancel",
		},
		{
			name:       "combined prints survive later script failure",
			code:       "print(" + strconv.Quote(long) + ")\nprint('last line')\nfail('later failure')",
			failed:     true,
			full:       long + "\nlast line\n",
			diagnostic: "later failure",
		},
		{
			name: "many small prints complete before a later side effect",
			code: "for i in range(2000):\n    print('line')",
			full: strings.Repeat(
				"line\n",
				2000,
			),
			completes: true,
		},
		{
			name: "large combined output completes before a later side effect",
			code: "for i in range(72):\n    print('x' * " + strconv.Itoa(maxTextBytes-1) + ")",
			full: strings.Repeat(
				strings.Repeat(
					"x",
					maxTextBytes-1,
				)+"\n",
				72,
			),
			completes: true,
		},
		{
			name: "text at preview boundary",
			code: "print('x' * " + strconv.Itoa(maxTextBytes-1) + ")",
			full: strings.Repeat(
				"x",
				maxTextBytes-1,
			) + "\n",
		},
		{
			name:        "overflow persistence failure does not interrupt script execution",
			code:        "print(" + strconv.Quote(long) + ")\nprint('finished printing')",
			full:        long + "\nfinished printing\n",
			saveFailure: true,
			failed:      true,
			completes:   true,
		},
		{
			name:       "oversized execution error is retained",
			code:       "fail(" + strconv.Quote(long) + ")",
			failed:     true,
			diagnostic: long,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				data := memoryKV{}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				host := hostFixture(
					t,
					emptyMCP{},
					runnerFunc(func(context.Context, dyson.Command) (dyson.CommandResult, error) {
						result := dyson.CommandResult{
							Stdout:     []byte(test.stdout),
							Stderr:     []byte(test.stderr),
							ReturnCode: test.exit,
						}
						if test.cancel {
							cancel()
							return result, context.Canceled
						}
						return result, nil
					}),
					func(config *Config) {
						config.KV = data
						if test.saveFailure {
							config.KV = failedOverflowStore{
								memoryKV: data,
							}
						}
					},
				)
				code := test.code
				if test.completes {
					code += "\nkv.store('completed', True)"
				}
				result, err := host.execute(
					ctx,
					toolRequest(
						starlarkToolName,
						"code",
						code,
					),
				)
				if err != nil || result.ToolResultError != test.failed || len(result.Blocks) != 1 {
					t.Fatalf(
						"execution: err=%v, failed=%v, blocks=%d; want failed=%v and one block",
						err,
						result.ToolResultError,
						len(result.Blocks),
						test.failed,
					)
				}
				output := result.Blocks[0].Content.String()
				if len(output) > maxTextBytes {
					t.Fatal("model-facing preview exceeded limit")
				}
				var keys []string
				for key := range data {
					if strings.HasPrefix(
						key,
						"overflow_",
					) {
						keys = append(
							keys,
							key,
						)
					}
				}
				overflow := len(test.full)+len(test.diagnostic) > maxTextBytes
				if test.saveFailure {
					if len(keys) != 0 || !strings.Contains(
						output,
						"could not save complete script output to KV: checkpoint unavailable",
					) {
						t.Fatal("save failure was not reported correctly")
					}
				} else if overflow {
					if len(keys) != 1 || !strings.Contains(
						output,
						keys[0],
					) {
						t.Fatal("missing complete-script overflow reference")
					}
					check := "saved = kv.get(" + strconv.Quote(keys[0]) + ")\nprint(saved == " + strconv.Quote(test.full) + ")"
					if test.diagnostic != "" {
						check = "saved = kv.get(" + strconv.Quote(keys[0]) + ")\nprint(saved.startswith(" + strconv.Quote(test.full) + ") and " + strconv.Quote(test.diagnostic) + " in saved)"
					}
					read, err := host.execute(
						t.Context(),
						toolRequest(
							starlarkToolName,
							"code",
							check,
						),
					)
					if err != nil || read.ToolResultError || read.Blocks[0].Content.String() != "True\n" {
						t.Fatalf(
							"complete output retrieval: %v, %v",
							read,
							err,
						)
					}
				} else if len(keys) != 0 || output != test.full {
					t.Fatalf(
						"output=%q, overflow keys=%v; want %q and no overflow",
						output,
						keys,
						test.full,
					)
				}
				if test.completes {
					read, err := host.execute(
						t.Context(),
						toolRequest(
							starlarkToolName,
							"code",
							"print(kv.get('completed'))",
						),
					)
					if err != nil || read.ToolResultError || read.Blocks[0].Content.String() != "True\n" {
						t.Fatal("output handling prevented a later script side effect")
					}
				}
			},
		)
	}
}

type failedOverflowStore struct {
	memoryKV
}

func (s failedOverflowStore) Store(ctx context.Context, key string, value []byte) error {
	if strings.HasPrefix(
		key,
		"overflow_",
	) {
		return errors.New("checkpoint unavailable")
	}
	return s.memoryKV.Store(
		ctx,
		key,
		value,
	)
}
