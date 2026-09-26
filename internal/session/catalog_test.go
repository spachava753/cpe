package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/session"
)

func TestList(t *testing.T) {
	for _, tc := range []struct {
		name     string
		canceled bool
		missing  bool
	}{
		{name: "matching sessions newest first without writer locks"}, {name: "missing directory", missing: true}, {name: "canceled", canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "sessions")
			if !tc.missing {
				for i, name := range []string{"first.jsonl", "second.jsonl", "other.jsonl"} {
					cwd := dir
					if name == "other.jsonl" {
						cwd = "elsewhere"
					}
					store, err := session.Open(filepath.Join(root, name), cwd, "test")
					if err != nil {
						t.Fatal(err)
					}
					defer store.Close()
					if _, err := store.Append("checkpoint", map[string]string{"label": name}); err != nil {
						t.Fatal(err)
					}
					if _, err := store.Append("message", map[string]string{"text": strings.Repeat("x", 2<<20)}); err != nil {
						t.Fatal(err)
					}
					title, err := session.ParseTitle("Title " + name)
					if err != nil {
						t.Fatal(err)
					}
					if err := store.SetTitle(title); err != nil {
						t.Fatal(err)
					}
					stamp := time.Unix(int64(i+1), 0)
					if err := os.Chtimes(store.Filename(), stamp, stamp); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, "bad.jsonl"), []byte("bad\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			got, err := session.List(ctx, root, dir)
			if tc.canceled {
				if err != context.Canceled {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.missing {
				if len(got) != 0 {
					t.Fatal(got)
				}
				return
			}
			if len(got) != 2 || filepath.Base(got[0].Path) != "second.jsonl" || got[1].Label != "first.jsonl" || got[1].Title.String() != "Title first.jsonl" {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestResume(t *testing.T) {
	for _, name := range []string{"missing", "empty", "existing", "locked"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "s.jsonl")
			if name == "empty" {
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "existing" || name == "locked" {
				s, err := session.Open(path, dir, "test")
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if name == "existing" {
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			s, err := session.Resume(path, dir, "test")
			if s != nil {
				defer s.Close()
			}
			if (err == nil) != (name == "existing") {
				t.Fatalf("%v", err)
			}
			if name == "missing" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("resume created missing file")
				}
			}
			if name == "empty" {
				data, _ := os.ReadFile(path)
				if len(data) != 0 {
					t.Fatal("resume populated empty file")
				}
			}
		})
	}
}
