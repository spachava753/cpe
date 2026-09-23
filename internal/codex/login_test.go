package codex

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"
)

const fixtureAuthorizationCode = "one-time-code"

// Local OAuth endpoints let tests exercise the real callback, exchange, and
// credential storage without consuming an account or opening a browser.
func fixtureLogin(path, base string) *loginClient {
	client := &http.Client{Timeout: time.Second, CheckRedirect: noRedirect}
	return &loginClient{
		store: &credentialStore{path: path, tokenURL: base + "/token", client: client}, client: client,
		authorizeURL: base + "/authorize", deviceURL: base + "/device", pollURL: base + "/poll",
		verificationURL: base + "/verify", deviceRedirect: base + "/device-callback",
		listenAddress: "127.0.0.1:0", minimumInterval: time.Millisecond,
	}
}

func TestBrowserLoginValidatesPKCEAndStateAndSavesPrivateCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpe", "auth.json")
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"own-account"}}`))
	access := "header." + claims + ".signature"
	var challenge, redirect string
	exchanges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			if r.Header.Get("Authorization") != "Bearer "+access || r.Header.Get("ChatGPT-Account-ID") != "own-account" {
				t.Error("generator did not use the newly saved login")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: "+`{"type":"response.output_text.delta","delta":"Ready."}`+"\n\ndata: "+`{"type":"response.completed","response":{"status":"completed"}}`+"\n\n")
			return
		}
		exchanges++
		if r.URL.Path != "/token" || r.Method != http.MethodPost {
			t.Error("incorrect token endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		actual := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(actual[:]) != challenge || r.Form.Get("code") != fixtureAuthorizationCode || r.Form.Get("redirect_uri") != redirect || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get(clientIDKey) != clientID {
			t.Error("invalid PKCE exchange")
		}
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"own-refresh","expires_in":3600}`, access)
	}))
	defer server.Close()
	l := fixtureLogin(path, server.URL)
	// This generator predates the login, as it does when CPE starts signed out.
	generator := newGenerator(l.store, http.DefaultTransport, server.URL+"/responses")
	l.openBrowser = func(ctx context.Context, target string) error {
		u, err := url.Parse(target)
		if err != nil {
			return err
		}
		q := u.Query()
		challenge, redirect = q.Get("code_challenge"), q.Get("redirect_uri")
		if q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid profile email offline_access" || q.Get("originator") != "cpe" {
			t.Error("invalid authorization request")
		}
		for _, callback := range []struct {
			state, code string
			status      int
		}{{"wrong-state", "bad-code", 400}, {q.Get("state"), "", 400}, {q.Get("state"), fixtureAuthorizationCode, 200}} {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirect+"?"+url.Values{"state": {callback.state}, "code": {callback.code}}.Encode(), nil)
			if err != nil {
				return err
			}
			resp, err := l.client.Do(req)
			if err != nil {
				return err
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != callback.status {
				t.Errorf("callback status %d", resp.StatusCode)
			}
		}
		return nil
	}
	var display strings.Builder
	if err := l.login(t.Context(), "browser", func(s string) { display.WriteString(s) }); err != nil {
		t.Fatal(err)
	}
	if exchanges != 1 {
		t.Fatalf("token exchanges: %d", exchanges)
	}
	response, err := generator.Generate(t.Context(), gai.GenerationRequest{Model: "login-fixture", Dialog: gai.Dialog{{Role: gai.User, Blocks: []gai.Block{gai.TextBlock("hello")}}}})
	if err != nil || len(response.Candidates) != 1 || len(response.Candidates[0].Blocks) == 0 || response.Candidates[0].Blocks[0].Content.String() != "Ready." {
		t.Fatalf("generator could not use the new login: %v", err)
	}
	for _, secret := range []string{access, "own-refresh", fixtureAuthorizationCode} {
		if strings.Contains(display.String(), secret) {
			t.Fatal("credential exposed in display")
		}
	}
	saved, err := l.store.credential(t.Context(), false)
	if err != nil || saved.accountID != "own-account" || saved.refresh != "own-refresh" {
		t.Fatalf("saved login invalid: %v", err)
	}
	if runtime.GOOS != windowsOS {
		file, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if file.Mode().Perm() != 0600 || dir.Mode().Perm() != 0700 {
			t.Fatal("credentials are not private")
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, redirect, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := l.client.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("callback listener remained open")
	}
}

func TestBrowserDenialAndCancellationPreserveExistingLogin(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprint("deny=", deny), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			before := writeAuthFixture(t, path, time.Now().Add(time.Hour))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			l := fixtureLogin(path, "http://127.0.0.1:1")
			l.openBrowser = func(ctx context.Context, target string) error {
				if !deny {
					cancel()
					return nil
				}
				u, _ := url.Parse(target)
				callback := u.Query().Get("redirect_uri") + "?" + url.Values{"state": {u.Query().Get("state")}, "error": {"access_denied"}, "error_description": {"secret-error-must-not-leak"}}.Encode()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, callback, nil)
				if err != nil {
					return err
				}
				resp, err := l.client.Do(req)
				if err != nil {
					return err
				}
				_ = resp.Body.Close()
				return nil
			}
			err := l.login(ctx, "browser", nil)
			if err == nil || strings.Contains(err.Error(), "secret-error") {
				t.Fatal("denial/cancellation failed")
			}
			if !deny && !errors.Is(err, context.Canceled) {
				t.Fatalf("unexpected cancellation: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("login failure replaced credentials")
			}
		})
	}
}

func TestDeviceLoginPollsAndExchangesBeforeSaving(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFixture(t, path, time.Now().Add(time.Hour))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"device-account"}}`))
	polls, exchanges := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			fmt.Fprint(w, `{"device_auth_id":"device-id","user_code":"ABC-123","interval":"0"}`)
		case "/poll":
			polls++
			var params map[string]string
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				t.Error(err)
			}
			if params["device_auth_id"] != "device-id" || params["user_code"] != "ABC-123" {
				t.Error("incorrect poll parameters")
			}
			if polls == 1 {
				w.WriteHeader(403)
				return
			}
			fmt.Fprint(w, `{"authorization_code":"device-code","code_verifier":"device-verifier"}`)
		case "/token":
			exchanges++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("code") != "device-code" || r.Form.Get("code_verifier") != "device-verifier" || !strings.HasSuffix(r.Form.Get("redirect_uri"), "/device-callback") {
				t.Error("bad device exchange")
			}
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"device-refresh","expires_in":3600}`, "header."+claims+".signature")
		default:
			t.Error("unexpected request")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	l := fixtureLogin(path, server.URL)
	var display string
	if err := l.login(t.Context(), "device", func(s string) { display = s }); err != nil {
		t.Fatal(err)
	}
	if polls != 2 || exchanges != 1 || !strings.Contains(display, "ABC-123") || strings.Contains(display, "device-verifier") {
		t.Fatal("incorrect device flow")
	}
	c, err := l.store.credential(t.Context(), false)
	if err != nil || c.accountID != "device-account" {
		t.Fatalf("device credential missing: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "other-secret") || !strings.Contains(string(data), "future") {
		t.Fatal("login lost other credential fields")
	}
}

func TestCanceledDeviceLoginStopsPollingAndSavesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/device" {
			fmt.Fprint(w, `{"device_auth_id":"device-id","user_code":"ABC-123","interval":0}`)
			return
		}
		polls++
		w.WriteHeader(403)
		cancel()
	}))
	defer server.Close()
	l := fixtureLogin(path, server.URL)
	if err := l.login(ctx, "device", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if polls != 1 {
		t.Fatalf("polls: %d", polls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled login saved credentials")
	}
}

func TestExchangeErrorsDoNotExposeCredentials(t *testing.T) {
	for _, body := range []string{`{"error":"secret-auth-code"}`, `{"access_token":"secret-access","refresh_token":"secret-refresh","expires_in":"secret-value"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
		l := fixtureLogin(filepath.Join(t.TempDir(), "auth.json"), server.URL)
		_, err := l.exchange(t.Context(), "secret-auth-code", "secret-verifier", "http://localhost/callback")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret-") {
			t.Fatal("invalid token response accepted or exposed")
		}
	}
}
