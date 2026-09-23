package session

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const formatVersion = 1

// Header fixes the working directory and interpreter format for a session.
type Header struct {
	Version int    `json:"version"`
	CWD     string `json:"cwd"`
	Runtime string `json:"runtime"`
}

// Entry is an immutable node in the JSONL tree. Data is specific to Type.
type Entry struct {
	ID        string          `json:"id"`
	ParentID  string          `json:"parentId,omitempty"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// Store owns a locked session file. Methods serialize access; returned entries
// are copies and callers must not mutate their Data slices.
type Store struct {
	mu      sync.Mutex
	file    *os.File
	lock    *flock.Flock
	path    string
	entries []Entry
	indexes map[string]int
	head    string
	header  Header
	failed  error
}

// Open creates or resumes path. cwd and runtime must match existing metadata.
// The caller must Close the store. Parent directories are private by default.
func Open(path, cwd, runtime string) (_ *Store, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(path)
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !locked {
		_ = lock.Close()
		return nil, fmt.Errorf("session is already open: %s", path)
	}
	defer func() {
		if err != nil {
			_ = lock.Close()
		}
	}()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
		}
	}()
	s := &Store{file: f, lock: lock, path: path, indexes: map[string]int{}}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	complete := bytes.LastIndexByte(data, '\n') + 1
	if len(data) > 0 && complete == 0 {
		return nil, errors.New("session has no complete header; refusing to overwrite it")
	}
	for line := range bytes.SplitSeq(data[:complete], []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("invalid session record %d: %w", len(s.entries)+1, err)
		}
		if err := s.validate(e); err != nil {
			return nil, err
		}
		s.accept(e)
	}
	if len(s.entries) > 0 {
		if err := json.Unmarshal(s.entries[0].Data, &s.header); err != nil {
			return nil, err
		}
		if s.header.Version != formatVersion || s.header.Runtime != runtime {
			return nil, errors.New("session interpreter version differs; start a new session")
		}
		if s.header.CWD != cwd {
			return nil, fmt.Errorf("session belongs to %s; run cpe from that directory", s.header.CWD)
		}
	} else if len(data) > 0 {
		return nil, errors.New("missing session header")
	}
	// Only repair a tail after validating the complete session and its metadata.
	if complete < len(data) {
		if err := f.Truncate(int64(complete)); err != nil {
			return nil, err
		}
		if err := f.Sync(); err != nil {
			return nil, err
		}
	}
	if err := f.Chmod(0600); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return nil, err
	}
	if len(s.entries) == 0 {
		s.header = Header{Version: formatVersion, CWD: cwd, Runtime: runtime}
		if _, err := s.Append("session", s.header); err != nil {
			return nil, err
		}
		// Persist the directory entry as well as the file on Unix filesystems.
		if d, err := os.Open(filepath.Dir(path)); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return s, nil
}

func (s *Store) validate(e Entry) error {
	if e.ID == "" || !json.Valid(e.Data) {
		return errors.New("invalid session entry")
	}
	if _, ok := s.indexes[e.ID]; ok {
		return fmt.Errorf("duplicate entry %s", e.ID)
	}
	if len(s.entries) == 0 {
		if e.Type != "session" || e.ParentID != "" {
			return errors.New("missing session header")
		}
	} else {
		if _, ok := s.indexes[e.ParentID]; !ok {
			return fmt.Errorf("missing parent %q", e.ParentID)
		}
		switch e.Type {
		case "message", "eval_start", "host_call", "host_result", "eval_end", "checkpoint", "compaction", "head", "request_start", "usage":
		default:
			return fmt.Errorf("unknown session entry type %q", e.Type)
		}
		if e.Type == "head" {
			parent := s.entries[s.indexes[e.ParentID]]
			if parent.Type != "checkpoint" && parent.Type != "session" {
				return errors.New("branches must start at a checkpoint")
			}
		} else if e.ParentID != s.head {
			return errors.New("unexpected parent: use a head entry to branch")
		}
	}
	return nil
}

func (s *Store) accept(e Entry) {
	s.indexes[e.ID] = len(s.entries)
	s.entries = append(s.entries, e)
	s.head = e.ID
}

// Append syncs a child of the current head before returning its ID. Once a write
// fails, further writes are rejected until the session is closed and reopened.
func (s *Store) Append(kind string, value any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.append(kind, value, s.head)
}

func (s *Store) append(kind string, value any, parent string) (string, error) {
	if s.file == nil {
		return "", errors.New("session is closed")
	}
	if s.failed != nil {
		return "", s.failed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	e := Entry{ID: rand.Text(), ParentID: parent, Type: kind, Timestamp: time.Now().UTC(), Data: data}
	if err := s.validate(e); err != nil {
		return "", err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	line = append(line, '\n')
	n, err := s.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = s.file.Sync()
	}
	if err != nil {
		s.failed = fmt.Errorf("session write failed: %w", err)
		return "", s.failed
	}
	s.accept(e)
	return e.ID, nil
}

// Branch switches the active path to a checkpoint without deleting any nodes.
func (s *Store) Branch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.indexes[id]; !ok {
		return fmt.Errorf("unknown checkpoint %q", id)
	}
	_, err := s.append("head", struct{}{}, id)
	return err
}

// Entries returns all entries in append order, including inactive branches.
func (s *Store) Entries() []Entry { s.mu.Lock(); defer s.mu.Unlock(); return slices.Clone(s.entries) }

// Path returns the active root-to-head path.
func (s *Store) Path() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var path []Entry
	for id := s.head; id != ""; {
		e := s.entries[s.indexes[id]]
		path = append(path, e)
		id = e.ParentID
	}
	slices.Reverse(path)
	return path
}

// Filename returns the on-disk JSONL path.
func (s *Store) Filename() string { return s.path }

// Close releases the file and exclusive lock. It is idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return errors.Join(err, s.lock.Close())
}
