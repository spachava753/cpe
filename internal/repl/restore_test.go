package repl

import (
	"path/filepath"
	"testing"

	"github.com/spachava753/cpe/internal/session"
)

func TestREPLRestore(t *testing.T) {
	const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII="
	const imageCode = `load("repl.star", "emit_image"); emit_image("` + pixel + `", mime_type="image/png"); x = 42; print("ab")`
	for _, test := range []struct {
		name    string
		format  toolArgumentFormat
		output  string
		wantErr bool
		code    string
		images  []Image
	}{
		{name: "legacy exact limit prefix", output: "[output truncated]\nab\n"},
		{name: "legacy exact limit without prefix", output: "ab\n"},
		{name: "new exact limit", format: losslessToolArguments, output: "ab\n"},
		{name: "new incorrect prefix", format: losslessToolArguments, output: "[output truncated]\nab\n", wantErr: true},
		{name: "different legacy output", output: "[output truncated]\nac\n", wantErr: true},
		{name: "image matches", format: losslessToolArguments, output: "ab\n", code: imageCode, images: []Image{{Data: pixel, MIMEType: "image/png"}}},
		{name: "missing image", format: losslessToolArguments, output: "ab\n", code: imageCode, wantErr: true},
		{name: "unexpected image", format: losslessToolArguments, output: "ab\n", images: []Image{{Data: pixel, MIMEType: "image/png"}}, wantErr: true},
		{name: "image MIME differs", format: losslessToolArguments, output: "ab\n", code: imageCode, images: []Image{{Data: pixel, MIMEType: "image/jpeg"}}, wantErr: true},
		{name: "image bytes differ", format: losslessToolArguments, output: "ab\n", code: imageCode, images: []Image{{Data: "Y2hhbmdlZA==", MIMEType: "image/png"}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "session.jsonl"), dir, Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			code := test.code
			if code == "" {
				code = `x = 42; print("ab")`
			}
			id, err := store.Append("eval_start", evalStart{CallID: "fixture", Code: code, OutputLimit: 3, ToolArguments: test.format})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Append("eval_end", Result{EvalID: id, CallID: "fixture", Output: test.output, Images: test.images, Committed: true}); err != nil {
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
