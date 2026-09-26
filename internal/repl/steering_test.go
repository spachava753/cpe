package repl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spachava753/dyson"

	"github.com/spachava753/cpe/internal/session"
)

type steeringCommandRunner struct {
	run func(context.Context) (dyson.CommandResult, error)
}

func (r steeringCommandRunner) RunCommand(ctx context.Context, _ dyson.Command) (dyson.CommandResult, error) {
	return r.run(ctx)
}

func TestREPLSteering(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		before     bool
	}{
		{"pure computation", "hold()\nwhile True:\n    n += 1", true},
		{"canceled host call", "value = hold()", false},
		{"successful host at cancellation", "value = hold()", true},
		{"successful subprocess at positional timeout", "load(\"subprocess.star\", \"subprocess\"); value = subprocess.run([\"fixture\"], None, None, None, None, True, False, None, 0.001)", false},
		{"successful subprocess at timeout", "load(\"subprocess.star\", \"subprocess\"); value = subprocess.run([\"fixture\"], capture_output=True, timeout=0.001)", false},
		{"subprocess timeout with steering", "load(\"subprocess.star\", \"subprocess\"); value = subprocess.run([\"fixture\"], capture_output=True, timeout=0.001)", false},
		{"captured subprocess output", "load(\"subprocess.star\", \"subprocess\"); value = subprocess.run([\"fixture\"], capture_output=True)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(context.Canceled)
			started := make(chan struct{})
			var calls atomic.Int32
			effect := func() {
				calls.Add(1)
				if err := os.WriteFile(filepath.Join(dir, "effect"), []byte("once"), 0600); err != nil {
					t.Error(err)
				}
			}
			tool := Tool{Name: "hold", Execute: func(ctx context.Context, _ map[string]any) (any, error) {
				effect()
				switch tc.name {
				case "pure computation":
					close(started)
					return 42, nil
				case "successful host at cancellation":
					cancel(ErrSteered)
					return 42, nil
				default:
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
			}}
			host := dyson.HostStdlibConfig(dir)
			host.CommandRunner = steeringCommandRunner{run: func(ctx context.Context) (dyson.CommandResult, error) {
				effect()
				if !strings.Contains(tc.name, "timeout") {
					close(started)
				}
				<-ctx.Done()
				if strings.Contains(tc.name, "timeout") {
					if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
						t.Error("builtin timeout lost")
					}
					cancel(ErrSteered)
				}
				if strings.HasPrefix(tc.name, "successful subprocess") {
					return dyson.CommandResult{Stdout: []byte("partial stdout\n"), Stderr: []byte("partial stderr\n")}, nil
				}
				return dyson.CommandResult{Stdout: []byte("partial stdout\n"), Stderr: []byte("partial stderr\n")}, ctx.Err()
			}}
			opts := Options{Store: store, CWD: dir, Tools: []Tool{tool}, Host: &host, Timeout: 3 * time.Second}
			r, err := New(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if result, err := r.Eval(t.Context(), "setup", `x = [1]; n = 0; value = 99`); err != nil || result.Error != "" {
				t.Fatal(result, err)
			}
			if tc.name != "successful host at cancellation" {
				go func() {
					select {
					case <-started:
						if tc.name == "pure computation" {
							time.Sleep(5 * time.Millisecond)
						}
						cancel(ErrSteered)
					case <-ctx.Done():
					}
				}()
			}
			code := `load("tools.star", "hold")
x.append(7)
print("printed before interruption")
load("repl.star", "emit_image")
emit_image("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII=", mime_type="image/png")
` + tc.code + "\nx.append(999)"
			result, err := r.Eval(ctx, "steered", code)
			if err != nil || !result.Committed || result.Interruption == nil || result.Interruption.BeforeOpcode != tc.before || !strings.Contains(result.Error, "steered prompt") || !strings.Contains(result.Output, "printed before interruption") || len(result.Images) != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if (tc.name == "captured subprocess output" || strings.Contains(tc.name, "timeout")) && (!strings.Contains(result.Output, "partial stdout") || !strings.Contains(result.Output, "partial stderr")) {
				t.Fatal("lost captured output", result.Output)
			}
			inspection, err := r.Eval(t.Context(), "inspect", `print(x, n, value)`)
			if err != nil || inspection.Error != "" || !strings.HasPrefix(inspection.Output, "[1, 7] ") || !strings.HasSuffix(inspection.Output, " 99\n") {
				t.Fatal("lost interrupted state", inspection, err)
			}
			failed, err := r.Eval(t.Context(), "ordinary failure", `x.append(99); fail("ordinary")`)
			if err != nil || failed.Committed || !strings.Contains(failed.Error, "rolled back") {
				t.Fatal(failed, err)
			}
			afterFailure, err := r.Eval(t.Context(), "after failure", `print(x, n, value)`)
			if err != nil || afterFailure.Output != inspection.Output {
				t.Fatal("rollback lost steered state", afterFailure, err)
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected effects", calls.Load())
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
			opts.Store = reopened
			opts.Tools = []Tool{{Name: "hold", Execute: func(context.Context, map[string]any) (any, error) {
				t.Error("repeated effect")
				return nil, errors.New("must not run")
			}}}
			opts.Host = &dyson.StdlibConfig{}
			restored, err := New(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			actual, err := restored.Eval(t.Context(), "inspect again", `print(x, n, value)`)
			if err != nil || actual.Error != "" || actual.Output != inspection.Output || calls.Load() != 1 {
				t.Fatal("replay differs", actual, inspection, err)
			}
		})
	}
}

func TestInterruptedReplayBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Result)
	}{
		{"exact", nil},
		{"zero steps", func(r *Result) { r.Interruption.Steps = 0 }},
		{"earlier opcode", func(r *Result) { r.Interruption.Steps-- }},
		{"later opcode", func(r *Result) { r.Interruption.Steps++ }},
		{"wrong phase", func(r *Result) { r.Interruption.BeforeOpcode = true }},
		{"uncommitted", func(r *Result) { r.Committed = false }},
		{"missing error", func(r *Result) { r.Error = "" }},
		{"changed output", func(r *Result) { r.Output = "invented" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			store, err := session.Open(path, dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(context.Canceled)
			tool := Tool{Name: "hold", Execute: func(context.Context, map[string]any) (any, error) { cancel(ErrSteered); return nil, context.Canceled }}
			r, err := New(t.Context(), Options{Store: store, CWD: dir, Tools: []Tool{tool}})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			outcome, err := r.Eval(ctx, "stop", `load("tools.star","hold"); x=[1]; print(x); hold(); x.append(99)`)
			if err != nil || outcome.Interruption == nil {
				t.Fatal(outcome, err)
			}
			entries := store.Entries()
			if tc.edit != nil {
				tc.edit(&outcome)
			}
			entries[len(entries)-1].Data, err = json.Marshal(outcome)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			var data []byte
			for _, entry := range entries {
				line, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, line...)
				data = append(data, '\n')
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			reopened, err := session.Open(path, dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			tool.Execute = func(context.Context, map[string]any) (any, error) {
				t.Error("replay executed host")
				return nil, errors.New("disabled")
			}
			restored, err := New(t.Context(), Options{Store: reopened, CWD: dir, Tools: []Tool{tool}})
			if tc.edit != nil {
				if err == nil {
					restored.Close()
					t.Fatal("accepted corrupt interruption")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			got, err := restored.Eval(t.Context(), "inspect", `print(x)`)
			if err != nil || got.Output != "[1]\n" {
				t.Fatal(got, err)
			}
		})
	}
}
