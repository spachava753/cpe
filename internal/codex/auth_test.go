package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

const fixtureRefresh = "fixture-refresh"

func writeAuthFixture(t *testing.T, path string, expires time.Time) []byte {
	t.Helper()
	data := fmt.Appendf(nil, `{"openai-codex":{"type":"oauth","access":"fixture-access","refresh":"fixture-refresh","expires":%d,"accountId":"fixture-account","future":{"keep":true}},"other":{"type":"api_key","key":"other-secret"}}`, expires.UnixMilli())
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCredentialStoreCredential(t *testing.T) {
	t.Run("invalid saved credentials", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := os.WriteFile(path, []byte(`{"openai-codex":{"expires":"secret-in-error"}}`), 0600); err != nil {
			t.Fatal(err)
		}
		store := &credentialStore{path: path}
		if _, err := store.credential(t.Context(), false); err == nil || strings.Contains(err.Error(), "secret-in-error") {
			t.Fatal("invalid credentials accepted or exposed")
		}
	})
	t.Run("serialized refresh preserves document", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		writeAuthFixture(t, path, time.Now().Add(-time.Hour))
		var refreshes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			refreshes.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("incorrect token request")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != fixtureRefresh || r.Form.Get(clientIDKey) != clientID {
				t.Error("incorrect refresh form")
			}
			fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
		}))
		defer server.Close()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				// Independent stores represent independently constructed providers.
				s := &credentialStore{path: path, tokenURL: server.URL, client: server.Client()}
				c, err := s.credential(t.Context(), true)
				if err != nil {
					t.Error(err)
					return
				}
				if c.access != "new-access" || c.refresh != "new-refresh" || c.accountID != "fixture-account" || c.expires <= time.Now().UnixMilli() {
					t.Error("incorrect refreshed credential")
				}
			})
		}
		wg.Wait()
		if refreshes.Load() != 1 {
			t.Fatalf("refresh calls: %d", refreshes.Load())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]map[string]json.RawMessage
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if string(saved["other"]["key"]) != `"other-secret"` || !strings.Contains(string(saved[providerKey]["future"]), "true") {
			t.Fatal("unrelated credential fields lost")
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != windowsOS && info.Mode().Perm() != 0600 {
			t.Fatal("credentials not private")
		}
		lock := flock.New(path + ".lock")
		held, err := lock.TryLockContext(t.Context(), time.Millisecond)
		if err != nil || !held {
			t.Fatal("lock still held", err)
		}
		_ = lock.Close()
	})
	t.Run("failed refresh preserves credentials", func(t *testing.T) {
		for _, test := range []struct {
			name, body string
			status     int
		}{
			{"rejected", `{"error":"fixture-refresh and other-secret"}`, 400},
			{"transient", `{"access_token":"secret-in-error"}`, 503},
			{"bad-json", `secret-in-error`, 200},
			{"bad-expiry", `{"access_token":"secret-in-error","expires_in":"fixture-refresh"}`, 200},
			{"missing-access", `{"refresh_token":"secret-in-error","expires_in":3600}`, 200},
			{"header-injection", `{"access_token":"secret-in-error\nInjected: value","expires_in":3600}`, 200},
		} {
			t.Run(test.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "auth.json")
				before := writeAuthFixture(t, path, time.Now().Add(-time.Hour))
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls++
					w.WriteHeader(test.status)
					fmt.Fprint(w, test.body)
				}))
				defer server.Close()
				s := &credentialStore{path: path, tokenURL: server.URL, client: server.Client()}
				_, err := s.credential(t.Context(), true)
				if err == nil || calls != 1 {
					t.Fatalf("error=%v calls=%d", err, calls)
				}
				for _, secret := range []string{fixtureRefresh, "other-secret", "secret-in-error"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatal("token endpoint leaked credentials in error")
					}
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != string(before) {
					t.Fatal("failed refresh changed auth file")
				}
			})
		}
	})
	t.Run("refresh without rotation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		writeAuthFixture(t, path, time.Now().Add(-time.Hour))
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"access_token":"new-access","expires_in":3600}`)
		}))
		defer server.Close()
		s := &credentialStore{path: path, tokenURL: server.URL, client: server.Client()}
		c, err := s.credential(t.Context(), true)
		if err != nil || c.refresh != fixtureRefresh {
			t.Fatalf("refresh failed: %v", err)
		}
	})
	t.Run("reload after login", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		before := writeAuthFixture(t, path, time.Now().Add(time.Hour))
		s := &credentialStore{path: path}
		first, err := s.credential(t.Context(), true)
		if err != nil || first.access != "fixture-access" {
			t.Fatalf("first read: %v", err)
		}
		after := strings.Replace(string(before), "fixture-access", "updated-by-login", 1)
		if err := os.WriteFile(path, []byte(after), 0600); err != nil {
			t.Fatal(err)
		}
		second, err := s.credential(t.Context(), true)
		if err != nil || second.access != "updated-by-login" {
			t.Fatalf("credential change was not reloaded: %v", err)
		}
	})
	t.Run("account claim and rotation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		writeAuthFixture(t, path, time.Now().Add(-time.Hour))
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"refreshed-account"}}`))
		token := "header." + claims + ".signature"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"rotated","expires_in":3600}`, token)
		}))
		defer server.Close()
		s := &credentialStore{path: path, tokenURL: server.URL, client: server.Client()}
		c, err := s.credential(t.Context(), true)
		if err != nil || c.accountID != "refreshed-account" || c.refresh != "rotated" {
			t.Fatalf("new account claim not retained: %v", err)
		}
	})
	t.Run("cancellation at refresh response", func(t *testing.T) {
		for _, test := range []struct {
			name, body string
			rotated    bool
		}{
			{"validated rotation is committed", `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":3600}`, true},
			{"invalid response is not committed", `{"access_token":"new-access","refresh_token":"rotated-refresh","expires_in":0}`, false},
		} {
			t.Run(test.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "auth.json")
				before := writeAuthFixture(t, path, time.Now().Add(-time.Hour))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				store := credentialStore{path: path, tokenURL: "https://fixture.test/token", client: &http.Client{Transport: authRoundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(&cancelAfterRead{reader: strings.NewReader(test.body), cancel: cancel})}, nil
				})}}
				_, err := store.credential(ctx, true)
				if err == nil || calls != 1 {
					t.Fatalf("canceled request: err=%v calls=%d", err, calls)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !test.rotated {
					if string(data) != string(before) {
						t.Fatal("invalid response changed credentials")
					}
					return
				}
				var document map[string]json.RawMessage
				if json.Unmarshal(data, &document) != nil || !strings.Contains(string(document["other"]), "other-secret") || !strings.Contains(string(document[providerKey]), "rotated-refresh") {
					t.Fatal("rotation or unrelated service lost")
				}
				next, err := store.credential(t.Context(), true)
				if err != nil || next.refresh != "rotated-refresh" || next.access != "new-access" || calls != 1 {
					t.Fatalf("replacement unavailable or obsolete token retried: %v, calls=%d", err, calls)
				}
			})
		}
	})
	t.Run("canceled refresh releases lock", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		before := writeAuthFixture(t, path, time.Now().Add(-time.Hour))
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			// Consume the form so net/http can observe the client's disconnect while
			// this handler deliberately leaves the response pending.
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			close(started)
			<-r.Context().Done()
		}))
		defer server.Close()
		s := &credentialStore{path: path, tokenURL: server.URL, client: server.Client()}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		finished := make(chan error, 1)
		go func() { _, err := s.credential(ctx, true); finished <- err }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("refresh did not start")
		}
		cancel()
		if err := <-finished; !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected cancellation: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("cancellation changed saved credential")
		}
		lock := flock.New(path + ".lock")
		held, err := lock.TryLockContext(t.Context(), time.Millisecond)
		if err != nil || !held {
			t.Fatal("lock still held", err)
		}
		_ = lock.Close()
	})
	t.Run("refresh refuses redirect", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		writeAuthFixture(t, path, time.Now().Add(-time.Hour))
		var leaked atomic.Bool
		sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			leaked.Store(true)
			w.WriteHeader(500)
		}))
		defer sink.Close()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		s := &credentialStore{path: path, tokenURL: server.URL, client: &http.Client{CheckRedirect: noRedirect}}
		if _, err := s.credential(t.Context(), true); err == nil || leaked.Load() {
			t.Fatal("token refresh followed a redirect")
		}
	})
}

type authRoundTrip func(*http.Request) (*http.Response, error)

func (f authRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type cancelAfterRead struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r *cancelAfterRead) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}

func TestNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if New(path) == nil {
		t.Fatal("constructor required login")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor touched credentials: %v", err)
	}
}

func TestAccountFromToken(t *testing.T) {
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`))
	for _, test := range []struct{ name, token, want string }{
		{"account claim", "header." + claims + ".signature", "account"},
		{"missing segments", "bad", ""},
		{"invalid base64", "a.%%%.b", ""},
		{"missing claim", "a." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".b", ""},
		{"malformed claims", "a." + base64.RawURLEncoding.EncodeToString([]byte(`{`)) + ".b", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := accountFromToken(test.token); got != test.want {
				t.Fatalf("accountFromToken() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHasCredential(t *testing.T) {
	valid := fmt.Sprintf(`{"type":"oauth","access":"fixture-access","refresh":"fixture-refresh","accountId":"account","expires":%d}`, time.Now().Add(time.Hour).UnixMilli())
	for _, test := range []struct {
		name, document string
		want           bool
	}{
		{name: "missing file"},
		{name: "other service only", document: `{"opencode-go":{"key":"fixture-key"}}`},
		{name: "valid OpenAI entry", document: `{"openai-codex":` + valid + `}`, want: true},
		{name: "both services", document: `{"opencode-go":{"key":"fixture-key"},"openai-codex":` + valid + `}`, want: true},
		{name: "invalid entry", document: `{"openai-codex":null}`},
		{name: "malformed document", document: `{"openai-codex":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if test.document != "" {
				if err := os.WriteFile(path, []byte(test.document), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := HasCredential(path); got != test.want {
				t.Fatalf("HasCredential = %v, want %v", got, test.want)
			}
			data, err := os.ReadFile(path)
			if test.document == "" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("created credentials")
				}
			} else if err != nil || string(data) != test.document {
				t.Fatal("modified credentials")
			}
		})
	}
}

func TestCredentialsNeverFollowSymlinks(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Unix symlink test")
	}
	dir := t.TempDir()
	target, path := filepath.Join(dir, "other.json"), filepath.Join(dir, "auth.json")
	before := writeAuthFixture(t, target, time.Now().Add(time.Hour))
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	s := &credentialStore{path: path}
	if _, err := s.credential(t.Context(), false); err == nil {
		t.Fatal("followed symlink")
	}
	if err := s.save(t.Context(), credential{}); err == nil {
		t.Fatal("overwrote symlink target")
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != string(before) {
		t.Fatal("changed external credentials")
	}
}
