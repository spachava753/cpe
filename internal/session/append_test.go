package session

import (
	"path/filepath"
	"testing"
)

func TestStoreAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.jsonl"), dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append("message", "first"); err == nil {
		t.Fatal("closed file write accepted")
	}
	if _, err := s.Append("message", "second"); err == nil {
		t.Fatal("poisoned writer accepted")
	}
	if len(s.Entries()) != 1 {
		t.Fatal("failed write advanced head")
	}
}
