package session_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/session"
)

func TestOpen(t *testing.T) {
	for _, test := range []struct {
		name, contents, cwd, runtime string
		wantErr                      bool
	}{
		{name: "new file"},
		{name: "complete header", contents: "header"},
		{name: "torn tail", contents: "header" + `{"id":"partial`},
		{name: "malformed complete record", contents: "header" + "bad JSON\n", wantErr: true},
		{name: "wrong working directory", contents: "header" + `{"id":"partial`, cwd: "elsewhere", wantErr: true},
		{name: "wrong runtime", contents: "header" + `{"id":"partial`, runtime: "different", wantErr: true},
		{name: "unterminated header", contents: `{"id":"partial`, wantErr: true},
		{name: "unrelated file", contents: "keep this file unchanged", wantErr: true},
		{name: "missing header", contents: "\n", wantErr: true},
		{name: "null record", contents: "null\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "s.jsonl")
			header := fmt.Sprintf(`{"id":"root","type":"session","data":{"version":1,"cwd":%q,"runtime":"test"}}`+"\n", dir)
			original := []byte(strings.Replace(test.contents, "header", header, 1))
			if test.contents != "" {
				if err := os.WriteFile(path, original, 0644); err != nil {
					t.Fatal(err)
				}
			}
			cwd, runtime := dir, "test"
			if test.cwd != "" {
				cwd = test.cwd
			}
			if test.runtime != "" {
				runtime = test.runtime
			}
			store, err := session.Open(path, cwd, runtime)
			if store != nil {
				t.Cleanup(func() { _ = store.Close() })
			}
			if (err != nil) != test.wantErr {
				t.Fatalf("Open() error = %v, want error %t", err, test.wantErr)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if test.wantErr {
				if !bytes.Equal(data, original) || info.Mode().Perm() != 0644 {
					t.Fatalf("failed open changed file: %q, mode %v", data, info.Mode())
				}
				return
			}
			if len(store.Entries()) != 1 || store.Path()[0].Type != "session" {
				t.Fatalf("unexpected entries: %+v", store.Entries())
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("session permissions = %v", info.Mode())
			}
			if test.contents != "" && string(data) != header {
				t.Fatalf("repaired data = %q, want %q", data, header)
			}
		})
	}
	t.Run("exclusive writer lock", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "s.jsonl")
		first, err := session.Open(path, dir, "test")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = first.Close() })
		if second, err := session.Open(path, dir, "test"); err == nil {
			_ = second.Close()
			t.Fatal("second writer acquired lock")
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		next, err := session.Open(path, dir, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err := next.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStoreBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s, err := session.Open(path, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if second, err := session.Open(path, dir, "test"); err == nil {
		_ = second.Close()
		t.Fatal("second writer acquired lock")
	}
	root := s.Path()[0].ID
	checkpoint, err := s.Append("checkpoint", map[string]string{"label": "first"})
	if err != nil {
		t.Fatal(err)
	}
	abandoned, err := s.Append("message", "old branch")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Branch(abandoned); err == nil {
		t.Fatal("branched at non-checkpoint")
	}
	if err := s.Branch(checkpoint); err != nil {
		t.Fatal(err)
	}
	current, err := s.Append("message", "new branch")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries()) != 5 {
		t.Fatal(s.Entries())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = session.Open(path, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	active := s.Path()
	if len(active) != 4 || active[0].ID != root || active[len(active)-1].ID != current {
		t.Fatal(active)
	}
	for _, e := range active {
		if e.ID == abandoned {
			t.Fatal("inactive branch leaked")
		}
	}
	if mode, err := os.Stat(path); err != nil || mode.Mode().Perm() != 0600 {
		t.Fatal("session permissions", mode, err)
	}
}
