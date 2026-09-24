package repl_test

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/repl"
	"github.com/spachava753/cpe/internal/session"
)

func TestREPLEmitImage(t *testing.T) {
	const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l1sAAAAASUVORK5CYII="
	raw, err := base64.StdEncoding.DecodeString(pixel)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, code, errorText string
		images                int
	}{
		{name: "MCP image", code: fmt.Sprintf(`emit_image({"type":"image","data":%q,"mimeType":"image/png"})`, pixel), images: 1},
		{name: "base64", code: fmt.Sprintf(`emit_image(%q, mime_type="image/png")`, pixel), images: 1},
		{name: "raw bytes", code: fmt.Sprintf(`emit_image(b%q, mime_type="image/png")`, raw), images: 1},
		{name: "multiple images", code: fmt.Sprintf(`for i in range(2): emit_image(%q, mime_type="image/png")`, pixel), images: 2},
		{name: "retained on failure", code: fmt.Sprintf(`emit_image(%q, mime_type="image/png"); fail("later failure")`, pixel), errorText: "later failure", images: 1},
		{name: "invalid base64", code: `emit_image("!", mime_type="image/png")`, errorText: "invalid base64"},
		{name: "empty", code: `emit_image("", mime_type="image/png")`, errorText: "empty image"},
		{name: "wrong type", code: `emit_image(123)`, errorText: "pass an MCP image"},
		{name: "missing MCP fields", code: `emit_image({"type":"image"})`, errorText: "requires string data"},
		{name: "wrong content type", code: fmt.Sprintf(`emit_image({"type":"audio","data":%q,"mimeType":"image/png"})`, pixel), errorText: "expected MCP image"},
		{name: "ambiguous MIME", code: fmt.Sprintf(`emit_image({"type":"image","data":%q,"mimeType":"image/png"}, mime_type="image/png")`, pixel), errorText: "omit mime_type"},
		{name: "missing MIME", code: fmt.Sprintf(`emit_image(%q)`, pixel), errorText: "mime_type must be"},
		{name: "unsupported MIME", code: fmt.Sprintf(`emit_image(%q, mime_type="image/svg+xml")`, pixel), errorText: "mime_type must be"},
		{name: "mismatched bytes", code: fmt.Sprintf(`emit_image(%q, mime_type="image/jpeg")`, pixel), errorText: "do not match"},
		{name: "size bound", code: `emit_image("A" * 30000000, mime_type="image/png")`, errorText: "exceed 20 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := session.Open(filepath.Join(dir, "s.jsonl"), dir, repl.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			r, err := repl.New(t.Context(), repl.Options{Store: store, CWD: dir, OutputLimit: 2})
			if err != nil {
				t.Fatal(err)
			}
			result, err := r.Eval(t.Context(), "image", `load("repl.star", "emit_image")`+"\n"+tc.code)
			if err != nil {
				t.Fatal(err)
			}
			if result.Committed != (tc.errorText == "") || !strings.Contains(result.Error, tc.errorText) || len(result.Images) != tc.images {
				t.Fatalf("result=%+v", result)
			}
			for _, img := range result.Images {
				if img.Data != pixel || img.MIMEType != "image/png" {
					t.Fatalf("image=%+v", img)
				}
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r, err = repl.New(t.Context(), repl.Options{Store: store, CWD: dir, OutputLimit: 2})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			next, err := r.Eval(t.Context(), "next", `print(1)`)
			if err != nil || next.Error != "" || len(next.Images) != 0 || next.Output != "1\n" {
				t.Fatalf("image leaked across evaluations: %+v %v", next, err)
			}
		})
	}
}
