package authstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEntryUpdate(t *testing.T) {
	for _, test := range []struct {
		name, document, next string
		setup, action        string
		wantRaw              string
		wantErr, wantWrite   bool
	}{
		{name: "new entry", next: `{"key":"new"}`, wantWrite: true},
		{name: "preserve other services and large numbers", document: `{"other":{"number":9007199254740993,"future":true}}`, next: `{"key":"new"}`, wantWrite: true},
		{name: "read without rewriting", document: `{"service":{"key":"current"}}`, wantRaw: `{"key":"current"}`},
		{name: "credential symlink rejected", setup: "symlink", next: `{}`, wantErr: true},
		{name: "credential directory rejected", setup: "directory", next: `{}`, wantErr: true},
		{name: "null document rejected", document: `null`, next: `{}`, wantErr: true},
		{name: "array document rejected", document: `[]`, next: `{}`, wantErr: true},
		{name: "malformed document hides contents", document: `{"fixture-secret":`, next: `{}`, wantErr: true},
		{name: "duplicate service rejected", document: `{"service":{},"service":{}}`, next: `{}`, wantErr: true},
		{name: "nested duplicate rejected", document: `{"service":{"key":"a","key":"b"}}`, next: `{}`, wantErr: true},
		{name: "trailing data rejected", document: `{} {}`, next: `{}`, wantErr: true},
		{name: "callback failure leaves file unchanged", document: `{"service":{}}`, action: "reject", wantRaw: `{}`, wantErr: true},
		{name: "invalid replacement hides contents", document: `{"service":{}}`, next: `{"fixture-secret":`, wantRaw: `{}`, wantErr: true},
		{name: "pre-canceled", document: `{"service":{}}`, action: "cancel before", next: `{}`, wantErr: true},
		{name: "canceled before commit", document: `{"service":{}}`, action: "cancel callback", next: `{"key":"new"}`, wantRaw: `{}`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			entry := Entry{Path: filepath.Join(dir, "auth.json"), Service: "service"}
			if test.document != "" {
				if err := os.WriteFile(entry.Path, []byte(test.document), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if test.setup == "directory" {
				if err := os.Mkdir(entry.Path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if test.setup == "symlink" {
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, []byte(`{"key":"untouched"}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, entry.Path); err != nil {
					t.Skip(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.action == "cancel before" {
				cancel()
			}
			err := entry.Update(ctx, func(raw json.RawMessage) (json.RawMessage, error) {
				if string(raw) != test.wantRaw {
					t.Error("wrong entry supplied to callback")
				}
				switch test.action {
				case "reject":
					return nil, errors.New("rejected")
				case "cancel callback":
					cancel()
				}
				if test.next == "" {
					return nil, nil
				}
				return json.RawMessage(test.next), nil
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("Update error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("parse error exposed secret")
			}
			if strings.HasPrefix(test.action, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			data, readErr := os.ReadFile(entry.Path)
			if test.wantWrite {
				if readErr != nil {
					t.Fatal(readErr)
				}
				var document, before map[string]json.RawMessage
				if err := json.Unmarshal(data, &document); err != nil {
					t.Fatal(err)
				}
				if test.document != "" {
					if err := json.Unmarshal([]byte(test.document), &before); err != nil {
						t.Fatal(err)
					}
				}
				for key, raw := range before {
					if key != entry.Service && !sameJSON(raw, document[key]) {
						t.Fatal("another service was changed")
					}
				}
				if !sameJSON(document[entry.Service], []byte(test.next)) {
					t.Fatal("wrong credential persisted")
				}
				info, err := os.Stat(entry.Path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
					t.Fatal("credentials not private")
				}
			} else if test.document != "" {
				if readErr != nil || string(data) != test.document {
					t.Fatal("original document changed")
				}
			} else if test.setup == "" && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal("unexpected auth document created")
			}
			if test.setup == "symlink" {
				data, err := os.ReadFile(filepath.Join(dir, "target"))
				if err != nil || string(data) != `{"key":"untouched"}` {
					t.Fatal("symlink target changed")
				}
			}
			files, err := filepath.Glob(filepath.Join(dir, ".cpe-auth-*"))
			if err != nil || len(files) != 0 {
				t.Fatal("temporary credentials left behind")
			}
		})
	}
	t.Run("cancellation while another service holds the lock", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		lock, err := acquireLock(t.Context(), path+".lock")
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		called := false
		err = (Entry{Path: path, Service: "other"}).Update(ctx, func(json.RawMessage) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`{}`), nil
		})
		if !errors.Is(err, context.DeadlineExceeded) || called {
			t.Fatalf("lock cancellation: %v, callback called=%v", err, called)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("canceled transaction wrote credentials")
		}
	})
	t.Run("concurrent service updates preserve every transaction", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		var group sync.WaitGroup
		for _, service := range []string{"openai-codex", "opencode-go", "github-copilot"} {
			for range 3 {
				group.Go(func() {
					entry := Entry{Path: path, Service: service}
					for range 5 {
						err := entry.Update(t.Context(), func(raw json.RawMessage) (json.RawMessage, error) {
							value := struct{ Count int }{}
							if raw != nil {
								if err := json.Unmarshal(raw, &value); err != nil {
									return nil, err
								}
							}
							value.Count++
							return json.Marshal(value)
						})
						if err != nil {
							t.Error(err)
							return
						}
					}
				})
			}
		}
		group.Wait()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]struct{ Count int }
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if len(document) != 3 {
			t.Fatal("a service was lost")
		}
		for service, value := range document {
			if value.Count != 15 {
				t.Errorf("%s: lost updates, count=%d", service, value.Count)
			}
		}
	})
}

func TestEntryRead(t *testing.T) {
	for _, test := range []struct {
		name, document, want string
		wantErr              bool
	}{
		{name: "missing"},
		{name: "current", document: `{"service":{"key":"new"}}`, want: `{"key":"new"}`},
		{name: "missing service", document: `{"other":{"key":"value"}}`},
		{name: "null entry", document: `{"service":null}`, want: `null`},
		{name: "malformed document", document: `{"fixture-secret":`, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			entry := Entry{Path: filepath.Join(dir, "auth.json"), Service: "service"}
			if test.document != "" {
				if err := os.WriteFile(entry.Path, []byte(test.document), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lock, err := acquireLock(t.Context(), entry.Path+".lock")
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			raw, err := entry.Read() // Must not wait behind an in-flight refresh.
			if (err != nil) != test.wantErr || string(raw) != test.want {
				t.Fatal("incorrect credential snapshot", err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("secret in error")
			}
			data, err := os.ReadFile(entry.Path)
			if test.document == "" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("read created auth.json")
				}
			} else if err != nil || string(data) != test.document {
				t.Fatal("read changed auth.json")
			}
		})
	}
}

func TestEntryRefresh(t *testing.T) {
	for _, test := range []struct {
		name               string
		replacement        string
		reject, wantCommit bool
	}{
		{name: "completed rotation survives cancellation", replacement: `{"key":"rotated"}`, wantCommit: true},
		{name: "no rotation remains cancellable"},
		{name: "failed refresh never commits", replacement: `{"key":"rotated"}`, reject: true},
		{name: "malformed replacement never commits", replacement: `{"key":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			const before = `{"service":{"key":"old"},"other":{"key":"keep"}}`
			if err := os.WriteFile(path, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := (Entry{Path: path, Service: "service"}).Refresh(ctx, func(json.RawMessage) (json.RawMessage, error) {
				cancel()
				if test.reject {
					return nil, errors.New("failed refresh")
				}
				if test.replacement == "" {
					return nil, nil
				}
				return json.RawMessage(test.replacement), nil
			})
			if (err == nil) != test.wantCommit {
				t.Fatalf("refresh: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantCommit {
				var document map[string]json.RawMessage
				if json.Unmarshal(data, &document) != nil || !sameJSON(document["service"], []byte(test.replacement)) || !sameJSON(document["other"], []byte(`{"key":"keep"}`)) {
					t.Fatal("replacement or unrelated service lost")
				}
			} else if string(data) != before {
				t.Fatal("failed refresh changed credentials")
			}
		})
	}
}

// Compare JSON without changing number representations through float64.
func sameJSON(a, b []byte) bool {
	var left, right bytes.Buffer
	return json.Compact(&left, a) == nil && json.Compact(&right, b) == nil && bytes.Equal(left.Bytes(), right.Bytes())
}

func TestAcquireLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json.lock")
	lock, err := acquireLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := acquireLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait: %v", err)
	}
	_ = lock.Close()
	next, err := acquireLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
}
