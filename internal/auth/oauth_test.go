package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func testToken(account string) string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"`+account+`"}}`)) + ".signature"
}

func TestExchange(t *testing.T) {
	valid := `{"access_token":"access-secret","refresh_token":"refresh-secret","id_token":"` + testToken("account") + `","expires_in":3600}`
	for _, test := range []struct {
		name    string
		status  int
		body    string
		cancel  bool
		wantErr bool
	}{
		{
			name:   "tokens",
			status: 200,
			body:   valid,
		},
		{
			name:   "missing lifetime",
			status: 200,
			body: strings.ReplaceAll(
				valid,
				`,"expires_in":3600`,
				"",
			),
		},
		{
			name:   "missing account",
			status: 200,
			body: strings.ReplaceAll(
				valid,
				testToken("account"),
				"invalid",
			),
			wantErr: true,
		},
		{
			name:   "missing refresh",
			status: 200,
			body: strings.ReplaceAll(
				valid,
				"refresh-secret",
				"",
			),
			wantErr: true,
		},
		{
			name:   "expired",
			status: 200,
			body: strings.ReplaceAll(
				valid,
				"3600",
				"0",
			),
			wantErr: true,
		},
		{
			name:   "overflow",
			status: 200,
			body: strings.ReplaceAll(
				valid,
				"3600",
				"9223372036854775807",
			),
			wantErr: true,
		},
		{
			name:    "malformed",
			status:  200,
			body:    "secret-in-body",
			wantErr: true,
		},
		{
			name:    "null",
			status:  200,
			body:    "null",
			wantErr: true,
		},
		{
			name:    "trailing",
			status:  200,
			body:    valid + " {}",
			wantErr: true,
		},
		{
			name:   "oversized",
			status: 200,
			body: strings.Repeat(
				"x",
				maxAuthResponse+1,
			),
			wantErr: true,
		},
		{
			name:   "invalid UTF8",
			status: 200,
			body: strings.ReplaceAll(
				valid,
				"access-secret",
				"access\xffsecret",
			),
			wantErr: true,
		},
		{
			name:    "denied",
			status:  400,
			body:    "access-secret refresh-secret secret-in-body",
			wantErr: true,
		},
		{
			name:    "redirect",
			status:  307,
			body:    "secret-in-body",
			wantErr: true,
		},
		{
			name:    "canceled",
			status:  200,
			body:    valid,
			cancel:  true,
			wantErr: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if request.URL.Path != "/oauth/token" || request.Method != "POST" {
						t.Error("wrong token request")
					}
					if err := request.ParseForm(); err != nil {
						t.Error(err)
					}
					for name, want := range map[string]string{
						"grant_type":    "authorization_code",
						"client_id":     openAIClientID,
						"code":          "code",
						"code_verifier": "verifier",
						"redirect_uri":  "callback",
					} {
						if request.PostForm.Get(name) != want {
							t.Errorf(
								"wrong form field: %s",
								name,
							)
						}
					}
					if test.status == 307 {
						writer.Header().Set(
							"Location",
							"/must-not-follow",
						)
					}
					writer.WriteHeader(test.status)
					_, _ = io.WriteString(
						writer,
						test.body,
					)
				}))
				defer server.Close()
				transport := server.Client()
				transport.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				}
				client := openAIClient{
					client: transport,
					issuer: server.URL,
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if test.cancel {
					cancel()
				}
				tokens, err := client.exchange(
					ctx,
					"code",
					"verifier",
					"callback",
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				if err != nil {
					for _, secret := range []string{
						"access-secret",
						"refresh-secret",
						"secret-in-body",
					} {
						if strings.Contains(
							err.Error(),
							secret,
						) {
							t.Error("error exposed credentials")
						}
					}
					if tokens != (OAuthCredentials{}) {
						t.Error("failure returned partial tokens")
					}
					return
				}
				if tokens.AccountID != "account" || tokens.AccessToken != "access-secret" || tokens.RefreshToken != "refresh-secret" {
					t.Error("wrong credentials")
				}
				if time.Until(tokens.ExpiresAt) < 59*time.Minute || time.Until(tokens.ExpiresAt) > time.Hour {
					t.Error("wrong expiry")
				}
			},
		)
	}
}

func TestCallbackHandler(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		path       string
		status     int
		wantResult bool
		wantError  bool
	}{
		{
			name:       "valid",
			method:     "GET",
			path:       "/auth/callback?state=expected&code=secret",
			status:     200,
			wantResult: true,
		},
		{
			name:       "denied",
			method:     "GET",
			path:       "/auth/callback?state=expected&error=denied&error_description=secret",
			status:     400,
			wantResult: true,
			wantError:  true,
		},
		{
			name:   "wrong state",
			method: "GET",
			path:   "/auth/callback?state=wrong&code=secret",
			status: 400,
		},
		{
			name:   "missing state",
			method: "GET",
			path:   "/auth/callback?code=secret",
			status: 400,
		},
		{
			name:   "repeated state",
			method: "GET",
			path:   "/auth/callback?state=expected&state=wrong&code=secret",
			status: 400,
		},
		{
			name:   "repeated code",
			method: "GET",
			path:   "/auth/callback?state=expected&code=secret&code=other",
			status: 400,
		},
		{
			name:   "mixed response",
			method: "GET",
			path:   "/auth/callback?state=expected&code=secret&error=denied",
			status: 400,
		},
		{
			name:   "missing code",
			method: "GET",
			path:   "/auth/callback?state=expected",
			status: 400,
		},
		{
			name:   "wrong method",
			method: "POST",
			path:   "/auth/callback?state=expected&code=secret",
			status: 405,
		},
		{
			name:   "wrong path",
			method: "GET",
			path:   "/cancel?state=expected",
			status: 404,
		},
		{
			name:   "invalid query",
			method: "GET",
			path:   "/auth/callback?state=%GG&code=secret",
			status: 400,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				results := make(
					chan authorizationResult,
					1,
				)
				handler := callbackHandler(
					"expected",
					results,
				)
				response := httptest.NewRecorder()
				handler.ServeHTTP(
					response,
					httptest.NewRequest(
						test.method,
						test.path,
						nil,
					),
				)
				if response.Code != test.status {
					t.Errorf(
						"status: %d",
						response.Code,
					)
				}
				if strings.Contains(
					response.Body.String(),
					"secret",
				) {
					t.Error("browser response exposed authorization data")
				}
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Error("response permits caching")
				}
				select {
				case result := <-results:
					if !test.wantResult || (result.err != nil) != test.wantError {
						t.Error("wrong callback result")
					}
					repeated := httptest.NewRecorder()
					handler.ServeHTTP(
						repeated,
						httptest.NewRequest(
							test.method,
							test.path,
							nil,
						),
					)
					if repeated.Code != 409 {
						t.Error("accepted replay")
					}
				default:
					if test.wantResult {
						t.Error("missing callback result")
					}
					// Invalid callbacks must not consume the legitimate login attempt.
					if test.method != "GET" || test.path != "/auth/callback?state=expected&code=secret" {
						valid := httptest.NewRecorder()
						handler.ServeHTTP(
							valid,
							httptest.NewRequest(
								"GET",
								"/auth/callback?state=expected&code=code",
								nil,
							),
						)
						if valid.Code != 200 {
							t.Error("invalid callback consumed attempt")
						}
					}
				}
			},
		)
	}
}

type loginLinkWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	link   chan string
}

func (writer *loginLinkWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	n, err := writer.buffer.Write(data)
	for line := range strings.SplitSeq(
		writer.buffer.String(),
		"\n",
	) {
		if strings.Contains(
			line,
			"/oauth/authorize?",
		) {
			select {
			case writer.link <- line:
			default:
			}
			break
		}
	}
	return n, err
}

func TestBrowserLogin(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel bool
	}{
		{
			name: "PKCE exchange",
		},
		{
			name:   "cancel",
			cancel: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				var authorization url.Values
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if err := request.ParseForm(); err != nil {
						t.Error(err)
					}
					digest := sha256.Sum256([]byte(request.PostForm.Get("code_verifier")))
					if base64.RawURLEncoding.EncodeToString(digest[:]) != authorization.Get("code_challenge") {
						t.Error("PKCE challenge mismatch")
					}
					if request.PostForm.Get("redirect_uri") != authorization.Get("redirect_uri") || request.PostForm.Get("code") != "code" {
						t.Error("wrong exchange values")
					}
					_, _ = fmt.Fprintf(
						writer,
						`{"access_token":"access","refresh_token":"refresh","id_token":"%s"}`,
						testToken("account"),
					)
				}))
				defer server.Close()
				client := openAIClient{
					client:  server.Client(),
					issuer:  server.URL,
					address: "127.0.0.1:0",
				}
				output := &loginLinkWriter{
					link: make(
						chan string,
						1,
					),
				}
				ctx, cancel := context.WithTimeout(
					context.Background(),
					5*time.Second,
				)
				defer cancel()
				finished := make(
					chan error,
					1,
				)
				go func() {
					_, err := client.browserLogin(
						ctx,
						output,
					)
					finished <- err
				}()
				var link string
				select {
				case link = <-output.link:
				case <-ctx.Done():
					t.Fatal("no login link")
				}
				parsed, err := url.Parse(link)
				if err != nil {
					t.Fatal(err)
				}
				authorization = parsed.Query()
				for name, want := range map[string]string{
					"response_type":         "code",
					"scope":                 "openid profile email offline_access",
					"client_id":             openAIClientID,
					"code_challenge_method": "S256",
					"originator":            "cpe",
				} {
					if authorization.Get(name) != want {
						t.Errorf(
							"wrong authorize field: %s",
							name,
						)
					}
				}
				redirect := authorization.Get("redirect_uri")
				if !strings.HasPrefix(
					redirect,
					"http://127.0.0.1:",
				) {
					t.Error("callback is not loopback-only")
				}
				if test.cancel {
					cancel()
				} else {
					response, err := http.Get(redirect + "?state=" + url.QueryEscape(authorization.Get("state")) + "&code=code")
					if err != nil {
						t.Fatal(err)
					}
					_ = response.Body.Close()
					if response.StatusCode != 200 {
						t.Error("callback failed")
					}
				}
				err = <-finished
				if test.cancel && !errors.Is(
					err,
					context.Canceled,
				) {
					t.Errorf(
						"cancel error: %v",
						err,
					)
				}
				if !test.cancel && err != nil {
					t.Fatal(err)
				}
				if response, err := http.Get(redirect); err == nil {
					response.Body.Close()
					t.Error("callback listener remains open")
				}
			},
		)
	}
}

func TestAccountID(t *testing.T) {
	for _, test := range []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "namespaced",
			token: testToken("account"),
			want:  "account",
		},
		{
			name:  "top level",
			token: "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"chatgpt_account_id":"top"}`)) + ".signature",
			want:  "top",
		},
		{
			name:  "malformed",
			token: "invalid",
		},
		{
			name:  "base64",
			token: "header.%.signature",
		},
		{
			name:  "claims",
			token: "header." + base64.RawURLEncoding.EncodeToString([]byte(`not JSON`)) + ".signature",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if got := accountID(test.token); got != test.want {
					t.Errorf(
						"account: %q",
						got,
					)
				}
			},
		)
	}
}
