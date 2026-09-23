package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeLockAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s, err := Open(path, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Open(path, dir, "test"); err == nil {
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
	s, err = Open(path, dir, "test")
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

func TestTornTailAndMalformedCompleteRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s, err := Open(path, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append("checkpoint", struct{}{}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"id":"partial`); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	s, err = Open(path, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries()) != 2 {
		t.Fatal(s.Entries())
	}
	_ = s.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "partial") {
		t.Fatal("torn tail retained")
	}
	if err := os.WriteFile(path, append(data, []byte("bad JSON\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path, dir, "test"); err == nil {
		_ = s.Close()
		t.Fatal("malformed record accepted")
	}
}

func TestWriteFailurePoisonsStore(t *testing.T) {
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

func TestMetadataMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s, err := Open(path, dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if s, err := Open(path, "elsewhere", "test"); err == nil {
		_ = s.Close()
		t.Fatal("cwd mismatch accepted")
	}
	if s, err := Open(path, dir, "different"); err == nil {
		_ = s.Close()
		t.Fatal("runtime mismatch accepted")
	}
}

func TestInvalidResumeDoesNotModifyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-session")
	original := []byte("keep this file unchanged")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path, dir, "test"); err == nil {
		_ = s.Close()
		t.Fatal("invalid file accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("invalid resume changed file: %q %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("invalid resume changed mode: %v %v", info, err)
	}
}
