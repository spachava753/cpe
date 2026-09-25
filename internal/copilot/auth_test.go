package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCredentialStoreCurrent(t *testing.T) {
	for _, test := range []struct {
		name     string
		expired  bool
		legacy   bool
		status   int
		endpoint string
		cancel   bool
	}{
		{name: "cached"}, {name: "legacy cached", legacy: true}, {name: "legacy refresh", expired: true, legacy: true}, {name: "legacy rejected refresh", expired: true, legacy: true, status: 401}, {name: "refresh", expired: true}, {name: "rejected refresh", expired: true, status: 401}, {name: "untrusted refresh endpoint", expired: true, endpoint: "https://evil.example"}, {name: "canceled lock", cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			client := newClient()
			store := credentialStore{dir: dir, client: client}
			saved := fixtureCredential()
			if test.expired {
				saved.ExpiresAt = time.Now().Add(-time.Minute).Unix()
			}
			if err := os.WriteFile(filepath.Join(dir, credentialFile), []byte(`{"other":{"key":"keep"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			legacy, err := json.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if test.legacy {
				if err := os.WriteFile(filepath.Join(dir, "copilot.json"), legacy, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := store.save(t.Context(), saved); err != nil {
				t.Fatal(err)
			}
			calls := 0
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != tokenURL || r.Header.Get("Authorization") != "token "+saved.GitHubToken {
					t.Error("wrong refresh destination or credential")
				}
				if test.status != 0 {
					return jsonResponse(test.status, saved.GitHubToken), nil
				}
				endpoint := test.endpoint
				if endpoint == "" {
					endpoint = "https://api.business.githubcopilot.com"
				}
				return jsonResponse(200, fmt.Sprintf(`{"token":"refreshed-secret","expires_at":%d,"endpoints":{"api":%q}}`, time.Now().Add(time.Hour).Unix(), endpoint)), nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			got, err := store.current(ctx)
			wantErr := test.status != 0 || test.endpoint != "" || test.cancel
			if (err != nil) != wantErr {
				t.Fatalf("current: %v", err)
			}
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("secret in refresh error")
			}
			persisted, readErr := store.read()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if wantErr || !test.expired {
				if persisted != saved {
					t.Fatal("unchanged credential overwritten")
				}
			} else if got.Token != "refreshed-secret" || persisted != got {
				t.Fatal("refreshed token not persisted")
			}
			if calls != 0 && (!test.expired || test.cancel) {
				t.Fatal("unnecessary refresh")
			}
			if test.expired && !test.cancel && calls != 1 {
				t.Fatalf("refresh was retried: %d", calls)
			}
			data, err := os.ReadFile(filepath.Join(dir, credentialFile))
			var document map[string]json.RawMessage
			if err != nil || json.Unmarshal(data, &document) != nil || !strings.Contains(string(document["other"]), "keep") {
				t.Fatal("other service lost", err)
			}
			if test.legacy {
				backup, err := os.ReadFile(filepath.Join(dir, "copilot.json"))
				if err != nil || string(backup) != string(legacy) {
					t.Fatal("legacy backup changed")
				}
				if !wantErr {
					var stored credential
					if json.Unmarshal(document[credentialName], &stored) != nil || stored != got {
						t.Fatal("credential not migrated")
					}
				} else if document[credentialName] != nil {
					t.Fatal("failed refresh migrated legacy credential")
				}
			}
		})
	}
	t.Run("concurrent stores exchange once", func(t *testing.T) {
		dir := t.TempDir()
		saved := fixtureCredential()
		saved.ExpiresAt = 1
		store := credentialStore{dir: dir}
		if err := store.save(t.Context(), saved); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		client := newClient()
		client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return jsonResponse(200, fmt.Sprintf(`{"token":"refreshed-secret","expires_at":%d}`, time.Now().Add(time.Hour).Unix())), nil
		})
		var group sync.WaitGroup
		for range 5 {
			group.Go(func() {
				s := credentialStore{dir: dir, client: client}
				if _, err := s.current(t.Context()); err != nil {
					t.Error(err)
				}
			})
		}
		group.Wait()
		if calls.Load() != 1 {
			t.Fatalf("exchanged %d times", calls.Load())
		}
	})
}

func TestCredentialStoreRead(t *testing.T) {
	data, _ := json.Marshal(fixtureCredential())
	valid := string(data)
	for _, test := range []struct {
		name, body      string
		symlink, legacy bool
		wantErr         bool
	}{
		{name: "valid", body: valid}, {name: "read legacy without migration", body: valid, legacy: true}, {name: "reject invalid legacy", body: strings.Replace(valid, `"version":1`, `"version":2`, 1), legacy: true, wantErr: true}, {name: "missing", wantErr: true}, {name: "null", body: `null`, wantErr: true}, {name: "malformed secret", body: `{"token":"fixture-secret`, wantErr: true}, {name: "unknown version", body: strings.Replace(valid, `"version":1`, `"version":2`, 1), wantErr: true}, {name: "untrusted host", body: strings.Replace(valid, apiURL, "https://example.com", 1), wantErr: true}, {name: "symlink", body: valid, symlink: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, credentialFile)
			body := `{"github-copilot":` + test.body + `}`
			if test.legacy {
				path = filepath.Join(dir, "copilot.json")
				body = test.body
			}
			if test.body != "" {
				if test.symlink {
					target := filepath.Join(dir, "target")
					if err := os.WriteFile(target, []byte(body), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, path); err != nil {
						t.Skip(err)
					}
				} else if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := (&credentialStore{dir: dir}).read()
			if (err != nil) != test.wantErr || HasCredential(dir) == test.wantErr {
				t.Fatalf("read: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("secret exposed")
			}
			if test.legacy {
				backup, err := os.ReadFile(path)
				if err != nil || string(backup) != body {
					t.Fatal("legacy backup changed")
				}
				if _, err := os.Stat(filepath.Join(dir, credentialFile)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("local availability check wrote auth.json")
				}
			}

		})
	}
}

func TestCredentialStoreSave(t *testing.T) {
	for _, test := range []struct {
		name            string
		cancel, symlink bool
	}{{name: "replace privately"}, {name: "cancel", cancel: true}, {name: "symlink", symlink: true}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, credentialFile)
			target := path
			if test.symlink {
				target = filepath.Join(dir, "target")
			}
			if err := os.WriteFile(target, []byte(`{"other":{"key":"unchanged"},"github-copilot":null}`), 0644); err != nil {
				t.Fatal(err)
			}
			if test.symlink {
				if err := os.Symlink(target, path); err != nil {
					t.Skip(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			store := credentialStore{dir: dir}
			err := store.save(ctx, fixtureCredential())
			if (err != nil) != (test.cancel || test.symlink) {
				t.Fatalf("save: %v", err)
			}
			data, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err != nil {
				if string(data) != `{"other":{"key":"unchanged"},"github-copilot":null}` {
					t.Fatal("failed save changed file")
				}
			} else {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
					t.Fatal("credentials not private")
				}
			}
			files, _ := filepath.Glob(filepath.Join(dir, ".cpe-auth-*"))
			if len(files) > 0 {
				t.Fatal("temporary credential left behind")
			}
		})
	}
}
