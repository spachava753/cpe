package textedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	t.Run("preserves surrounding spaces in path", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), " file with spaces.txt ")
		out, err := apply(input{Path: path, NewText: "hello"})
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if out.Path != path {
			t.Fatalf("resolved path = %q, want %q", out.Path, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file at exact path with spaces: %v", err)
		}
	})
	t.Run("creates new file and parents", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nested", "file.txt")
		out, err := apply(input{Path: path, NewText: "hello"})
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if out.Operation != "created" || out.Path != path {
			t.Fatalf("unexpected output: %#v", out)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading created file: %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("content = %q, want hello", string(got))
		}
	})
	t.Run("create fails when file exists", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := apply(input{Path: path, NewText: "new"})
		if err == nil || !strings.Contains(err.Error(), "file already exists") {
			t.Fatalf("expected file exists error, got %v", err)
		}
	})
	t.Run("replaces exactly one match and preserves permissions", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(path, []byte("alpha beta gamma"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := apply(input{Path: path, OldText: "beta", NewText: "BETA"})
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if out.Operation != "modified" || out.Replacements != 1 {
			t.Fatalf("unexpected output: %#v", out)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "alpha BETA gamma" {
			t.Fatalf("content = %q", string(got))
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
		}
	})
	t.Run("replace rejects missing and duplicate matches", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(path, []byte("one two one"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := apply(input{Path: path, OldText: "missing", NewText: "x"})
		if err == nil || !strings.Contains(err.Error(), "old_text not found") {
			t.Fatalf("expected missing old_text error, got %v", err)
		}

		_, err = apply(input{Path: path, OldText: "one", NewText: "x"})
		if err == nil || !strings.Contains(err.Error(), "appears 2 times") {
			t.Fatalf("expected duplicate old_text error, got %v", err)
		}
	})
	t.Run("replace rejects overlapping duplicate matches", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(path, []byte("aaa"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := apply(input{Path: path, OldText: "aa", NewText: "x"})
		if err == nil || !strings.Contains(err.Error(), "appears 2 times") {
			t.Fatalf("expected overlapping duplicate old_text error, got %v", err)
		}
	})
	t.Run("replace rejects symlink", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		target := filepath.Join(root, "target.txt")
		link := filepath.Join(root, "link.txt")
		if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}

		_, err := apply(input{Path: link, OldText: "old", NewText: "new"})
		if err == nil || !strings.Contains(err.Error(), "path is a symlink") {
			t.Fatalf("expected symlink error, got %v", err)
		}

		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "old" {
			t.Fatalf("target content = %q, want old", string(got))
		}
		if _, err := os.Lstat(link); err != nil {
			t.Fatalf("symlink was removed: %v", err)
		}
	})
	t.Run("replace rejects invalid utf 8", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file.bin")
		if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := apply(input{Path: path, OldText: "x", NewText: "y"})
		if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
			t.Fatalf("expected invalid UTF-8 error, got %v", err)
		}
	})
}
