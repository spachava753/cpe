package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func fixtureCredential() credential {
	return credential{Version: 1, GitHubToken: "fixture-github-secret", Token: "fixture-copilot-secret", ExpiresAt: time.Now().Add(time.Hour).Unix(), APIURL: apiURL}
}

const fixtureConfig = `{"default_model":"mine","models":{"mine":{"provider":"codex","id":"fixture"}}}`
const fixtureModel = `{"id":"fixture","model_picker_enabled":true,"supported_endpoints":["/chat/completions"],"capabilities":{"supports":{"tool_calls":true,"streaming":true},"limits":{"max_context_window_tokens":200000,"max_prompt_tokens":180000,"max_output_tokens":32000}}}`

func TestLoginClientAuthorize(t *testing.T) {
	for _, test := range []struct {
		name     string
		polls    []string
		device   string
		wantWait []time.Duration
		wantErr  bool
		cancel   bool
	}{
		{name: "authorized", polls: []string{`{"access_token":"github-secret","token_type":"bearer"}`}, wantWait: []time.Duration{5 * time.Second}},
		{name: "pending and persistent slowdown", polls: []string{`{"error":"authorization_pending"}`, `{"error":"slow_down","interval":20}`, `{"error":"authorization_pending"}`, `{"access_token":"github-secret","token_type":"bearer"}`}, wantWait: []time.Duration{5 * time.Second, 5 * time.Second, 20 * time.Second, 20 * time.Second}},
		{name: "denied", polls: []string{`{"error":"access_denied"}`}, wantErr: true},
		{name: "expired", polls: []string{`{"error":"expired_token"}`}, wantErr: true},
		{name: "unknown error omits body", polls: []string{`{"error":"github-secret","error_description":"github-secret"}`}, wantErr: true},
		{name: "missing token type", polls: []string{`{"access_token":"github-secret"}`}, wantErr: true},
		{name: "malformed response", polls: []string{`{"github-secret"`}, wantErr: true},
		{name: "untrusted verification URL", device: `{"device_code":"device-secret","user_code":"CPE-TEST","verification_uri":"https://example.com","expires_in":900,"interval":5}`, wantErr: true},
		{name: "invalid expiry", device: `{"device_code":"device-secret","user_code":"CPE-TEST","verification_uri":"https://github.com/login/device","expires_in":0,"interval":5}`, wantErr: true},
		{name: "cancellation", cancel: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			polls := 0
			var waits []time.Duration
			var display string
			client := newClient()
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" {
					t.Error("credential sent before authorization")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("client_id") != clientID {
					t.Error("wrong OAuth client")
				}
				if r.URL.String() == deviceURL {
					body := test.device
					if body == "" {
						body = `{"device_code":"device-secret","user_code":"CPE-TEST","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`
					}
					return jsonResponse(200, body), nil
				}
				if r.URL.String() != pollURL || r.Form.Get("device_code") != "device-secret" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
					t.Error("wrong poll")
				}
				if polls >= len(test.polls) {
					t.Fatal("unexpected poll")
				}
				body := test.polls[polls]
				polls++
				return jsonResponse(200, body), nil
			})
			l := loginClient{client: client, wait: func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return ctx.Err() }}
			got, err := l.authorize(ctx, func(text string) {
				display += text
				if test.cancel {
					cancel()
				}
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("authorize: %v", err)
			}
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "github-secret") || strings.Contains(err.Error(), "device-secret")) {
				t.Fatal("secret in error")
			}
			if !test.wantErr && (got != "github-secret" || !reflect.DeepEqual(waits, test.wantWait)) {
				t.Fatalf("token or polling mismatch: waits=%v", waits)
			}
			if strings.Contains(display, "device-secret") || strings.Contains(display, "github-secret") {
				t.Fatal("secret in display")
			}
			if test.device == "" && (!strings.Contains(display, verificationURL) || !strings.Contains(display, "CPE-TEST")) {
				t.Fatal("missing user instructions")
			}
		})
	}
}

func TestLoginClientLogin(t *testing.T) {
	for _, test := range []struct {
		name              string
		status            int
		catalog           string
		badConfig, cancel bool
	}{
		{name: "success and replacement"},
		{name: "subscription rejection", status: 403},
		{name: "empty catalog", catalog: `{"data":[]}`},
		{name: "invalid configuration", badConfig: true},
		{name: "cancel before poll", cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			body := fixtureConfig
			if test.badConfig {
				body = `{`
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			store := credentialStore{dir: dir}
			old := fixtureCredential()
			old.GitHubToken = "old-github"
			if err := store.save(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			githubToken := "new-github-secret"
			client := newClient()
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.URL.String() {
				case deviceURL:
					return jsonResponse(200, `{"device_code":"device-secret","user_code":"CPE-TEST","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`), nil
				case pollURL:
					return jsonResponse(200, fmt.Sprintf(`{"access_token":%q,"token_type":"bearer"}`, githubToken)), nil
				case tokenURL:
					if r.Header.Get("Authorization") != "token "+githubToken {
						t.Error("wrong exchange credential")
					}
					if test.status != 0 {
						return jsonResponse(test.status, githubToken), nil
					}
					return jsonResponse(200, fmt.Sprintf(`{"token":"new-copilot-secret","expires_at":%d,"endpoints":{"api":%q}}`, time.Now().Add(time.Hour).Unix(), apiURL)), nil
				case apiURL + "/models":
					if r.Header.Get("Authorization") != "Bearer new-copilot-secret" {
						t.Error("wrong catalog credential")
					}
					catalog := test.catalog
					if catalog == "" {
						catalog = `{"data":[` + fixtureModel + `]}`
					}
					return jsonResponse(200, catalog), nil
				default:
					t.Fatalf("unexpected URL %s", r.URL)
					return nil, nil
				}
			})
			l := loginClient{client: client, wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}
			profiles, err := l.login(ctx, dir, func(string) {
				if test.cancel {
					cancel()
				}
			})
			failed := test.status != 0 || test.catalog != "" || test.badConfig || test.cancel
			if (err != nil) != failed {
				t.Fatalf("login error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), githubToken) {
				t.Fatal("secret in error")
			}
			saved, readErr := store.read()
			if readErr != nil {
				t.Fatal(readErr)
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "config.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if failed {
				if saved.GitHubToken != old.GitHubToken || string(data) != body {
					t.Fatal("failed login mutated configuration")
				}
				return
			}
			if calls != 4 || saved.GitHubToken != githubToken || len(profiles) != 2 || profiles[credentialName+"/fixture"].Credential != credentialName || strings.Contains(string(data), "secret") {
				t.Fatal("login did not import and privately save")
			}
			var document struct {
				Default string `json:"default_model"`
			}
			_ = json.Unmarshal(data, &document)
			if document.Default != "mine" {
				t.Fatal("default changed")
			}
			githubToken = "replacement-github"
			if _, err := l.login(ctx, dir, nil); err != nil {
				t.Fatal(err)
			}
			saved, err = store.read()
			if err != nil || saved.GitHubToken != githubToken {
				t.Fatal("replacement unavailable")
			}
		})
	}
}
