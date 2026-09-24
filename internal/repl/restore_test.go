package repl

import (
	"path/filepath"
	"testing"

	"github.com/spachava753/cpe/internal/session"
)

func TestREPLRestore(t *testing.T) {
	for _, test := range []struct {
		name    string
		format  toolArgumentFormat
		output  string
		wantErr bool
	}{
		{name: "legacy exact limit prefix", output: "[output truncated]\nab\n"},
		{name: "legacy exact limit without prefix", output: "ab\n"},
		{name: "new exact limit", format: losslessToolArguments, output: "ab\n"},
		{name: "new incorrect prefix", format: losslessToolArguments, output: "[output truncated]\nab\n", wantErr: true},
		{name: "different legacy output", output: "[output truncated]\nac\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			id, err := store.Append("eval_start", evalStart{CallID: "fixture", Code: `x = 42; print("ab")`, OutputLimit: 3, ToolArguments: test.format})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Append("eval_end", Result{EvalID: id, CallID: "fixture", Output: test.output, Committed: true}); err != nil {
				t.Fatal(err)
			}
			r, err := New(t.Context(), Options{Store: store, CWD: dir})
			if r != nil {
				defer r.Close()
			}
			if (err != nil) != test.wantErr {
				t.Fatalf("restore error = %v, want error %t", err, test.wantErr)
			}
			if err != nil {
				return
			}
			result, err := r.Eval(t.Context(), "continued", `print(x)`)
			if err != nil || result.Error != "" || result.Output != "42\n" {
				t.Fatalf("restored state: %+v, %v", result, err)
			}
		})
	}
}
