package repl

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spachava753/dyson"
	"github.com/spachava753/starlarkx/starlark"
	"github.com/spachava753/starlarkx/starlarkstruct"

	"github.com/spachava753/cpe/internal/session"
)

func TestSubprocessReplay(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"keyword timeout", `p = subprocess.run(["fixture"], capture_output=True, text=True, timeout=1)`},
		{"positional timeout", `p = subprocess.run(["fixture"], None, None, None, None, True, False, None, 1, False, True)`},
		{"no timeout", `p = subprocess.run(["fixture"], capture_output=True, text=True)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			calls := 0
			host := dyson.StdlibConfig{CommandRunner: steeringCommandRunner{run: func(ctx context.Context) (dyson.CommandResult, error) {
				calls++
				_, deadline := ctx.Deadline()
				if tc.name != "no timeout" && !deadline {
					t.Error("live command lost builtin deadline")
				}
				return dyson.CommandResult{Stdout: []byte("complete"), ReturnCode: 7}, nil
			}}}
			r, err := New(t.Context(), Options{Store: store, CWD: dir, Host: &host})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			result, err := r.Eval(t.Context(), "run", `load("subprocess.star", "subprocess"); `+tc.code+`; print(p.stdout,p.returncode)`)
			if err != nil || result.Error != "" || result.Output != "complete 7\n" || calls != 1 {
				t.Fatal(result, err, calls)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := session.Open(path, dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			restored, err := New(t.Context(), Options{Store: reopened, CWD: dir, Host: &dyson.StdlibConfig{}})
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			got, err := restored.Eval(t.Context(), "inspect", `print(p.stdout,p.returncode)`)
			if err != nil || got.Error != "" || got.Output != result.Output || calls != 1 {
				t.Fatal(got, err, calls)
			}
		})
	}
}

func TestJournalSubprocessModule(t *testing.T) {
	for _, tc := range []struct {
		name             string
		invalid, missing bool
	}{
		{name: "intact host error"},
		{name: "missing runner pair", missing: true},
		{name: "argument error without runner", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			j := &journal{ctx: t.Context(), store: store, active: true}
			calls := 0
			source := dyson.NewStdlib(dyson.StdlibConfig{CommandRunner: commandRunner{j: j, cwd: dir, host: steeringCommandRunner{run: func(context.Context) (dyson.CommandResult, error) {
				calls++
				return dyson.CommandResult{}, context.Canceled
			}}}})
			defer source.Close()
			module, ok := j.subprocessModule(source)["subprocess.star"]["subprocess"].(*starlarkstruct.Module)
			if !ok {
				t.Fatal("missing subprocess module")
			}
			args := starlark.Tuple{starlark.NewList([]starlark.Value{starlark.String("fixture")})}
			if tc.invalid {
				args = starlark.Tuple{starlark.None}
			}
			_, originalErr := starlark.Call(&starlark.Thread{}, module.Members["run"], args, nil)
			if originalErr == nil || j.fatal != nil {
				t.Fatal("missing builtin error", originalErr, j.fatal)
			}
			expectedCalls := 1
			if tc.invalid {
				expectedCalls = 0
			}
			if calls != expectedCalls {
				t.Fatal("unexpected live runner calls", calls)
			}
			j.records = store.Entries()[1:]
			if tc.missing {
				j.records = j.records[2:]
			}
			j.replay = true
			j.position = 0
			_, replayErr := starlark.Call(&starlark.Thread{}, module.Members["run"], args, nil)
			if tc.missing {
				if j.fatal == nil || replayErr == nil {
					t.Fatal("accepted missing host records")
				}
			} else if j.fatal != nil || replayErr == nil || replayErr.Error() != originalErr.Error() || j.position != len(j.records) {
				t.Fatal("replay differs", replayErr, j.fatal)
			}
			if calls != expectedCalls {
				t.Fatal("repeated live runner")
			}
		})
	}
}
