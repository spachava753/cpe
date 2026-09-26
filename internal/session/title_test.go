package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spachava753/cpe/internal/session"
)

func TestParseTitle(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		invalid          bool
	}{
		{name: "trim", text: "  Investigate auth  ", want: "Investigate auth"},
		{name: "unicode limit", text: strings.Repeat("界", 120), want: strings.Repeat("界", 120)},
		{name: "empty", invalid: true}, {name: "too long", text: strings.Repeat("a", 121), invalid: true},
		{name: "newline", text: "one\ntwo", invalid: true}, {name: "escape", text: "one\x1btwo", invalid: true},
		{name: "unicode separator", text: "one\u2028two", invalid: true}, {name: "invalid utf8", text: "\xff", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title, err := session.ParseTitle(tc.text)
			if (err != nil) != tc.invalid || title.String() != tc.want {
				t.Fatalf("title=%q err=%v", title.String(), err)
			}
		})
	}
}

func TestStoreSetTitle(t *testing.T) {
	for _, name := range []string{"branch and reopen", "zero rejected", "invalid journal rejected"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "title.jsonl")
			s, err := session.Open(path, dir, "test")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			root := s.Entries()[0].ID
			if s.Title().String() != "" {
				t.Fatal("new session has title")
			}
			if name == "zero rejected" {
				if err := s.SetTitle(session.Title{}); err == nil {
					t.Fatal("accepted zero title")
				}
				return
			}
			for _, text := range []string{"Generated title", "Manual title"} {
				title, err := session.ParseTitle(text)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.SetTitle(title); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Branch(root); err != nil {
				t.Fatal(err)
			}
			if s.Title().String() != "Manual title" {
				t.Fatal("branch reverted title")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if name == "invalid journal rejected" {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = []byte(strings.Replace(string(data), `"title":"Manual title"`, `"title":""`, 1))
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := session.Resume(path, dir, "test")
			if name == "invalid journal rejected" {
				if err == nil {
					reopened.Close()
					t.Fatal("accepted invalid title")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if reopened.Title().String() != "Manual title" || reopened.Filename() != path {
				t.Fatal("title/identity lost on reopen")
			}
		})
	}
}
