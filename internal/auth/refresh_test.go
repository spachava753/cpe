package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spf13/afero"
)

type refreshTransport func(*http.Request) (*http.Response, error)

func (f refreshTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type refreshFaultFS struct {
	afero.Fs
	failures int
}

func (f *refreshFaultFS) Rename(from, to string) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("injected publication failure")
	}
	return f.Fs.Rename(
		from,
		to,
	)
}

func TestRefreshOpenAI(t *testing.T) {
	for _, test := range []struct {
		name         string
		body         string
		operation    string
		wantAccess   string
		failures     int
		saveFailures int
		expires      time.Duration
		wantCalls    int
		wantError    bool
	}{
		{
			name:       "rotated tokens",
			body:       `{"access_token":"renewed","refresh_token":"rotated","id_token":"renewed-id","expires_in":3600}`,
			expires:    time.Minute,
			wantCalls:  1,
			wantAccess: "renewed",
		},
		{
			name:       "optional tokens retained",
			body:       `{"access_token":"renewed","expires_in":3600}`,
			expires:    time.Minute,
			wantCalls:  1,
			wantAccess: "renewed",
		},
		{
			name:       "retry until successful",
			body:       `{"access_token":"renewed","expires_in":3600}`,
			failures:   2,
			expires:    time.Minute,
			wantCalls:  3,
			wantAccess: "renewed",
		},
		{
			name:       "stop retrying at expiry",
			failures:   100,
			expires:    5 * time.Second,
			wantCalls:  3,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "expired on startup can refresh",
			body:       `{"access_token":"renewed","expires_in":3600}`,
			expires:    -time.Hour,
			wantCalls:  1,
			wantAccess: "renewed",
		},
		{
			name:       "expired on startup does not retry failure",
			failures:   100,
			expires:    -time.Hour,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "missing access token rejected",
			body:       `{"refresh_token":"rotated","expires_in":3600}`,
			expires:    -time.Hour,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "invalid expiry rejected",
			body:       `{"access_token":"renewed","expires_in":0}`,
			expires:    -time.Hour,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "account change rejected",
			body:       `{"access_token":"renewed","id_token":"` + testToken("different-account") + `","expires_in":3600}`,
			expires:    -time.Hour,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "transport secrets stay private",
			operation:  "transport error",
			expires:    -time.Hour,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "refresh redirects are not followed",
			operation:  "redirect",
			expires:    -time.Hour,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "cancel refresh",
			operation:  "cancel",
			expires:    time.Minute,
			wantCalls:  1,
			wantError:  true,
			wantAccess: "original",
		},
		{
			name:       "bounded attempt retries without waiting for expiry",
			operation:  "timeout once",
			body:       `{"access_token":"renewed","expires_in":3600}`,
			expires:    time.Minute,
			wantCalls:  2,
			wantAccess: "renewed",
		},
		{
			name:      "logout during refresh wins",
			operation: "logout",
			body:      `{"access_token":"renewed","expires_in":3600}`,
			expires:   time.Minute,
			wantCalls: 1,
		},
		{
			name:       "new login during refresh wins",
			operation:  "login",
			body:       `{"access_token":"renewed","expires_in":3600}`,
			expires:    time.Minute,
			wantCalls:  1,
			wantAccess: "new-login",
		},
		{
			name:       "other provider update preserved",
			operation:  "other provider",
			body:       `{"access_token":"renewed","expires_in":3600}`,
			expires:    time.Minute,
			wantCalls:  1,
			wantAccess: "renewed",
		},
		{
			name:       "rotated tokens survive old access token expiry while saving",
			operation:  "save failure",
			body:       `{"access_token":"renewed","refresh_token":"rotated","expires_in":3600}`,
			expires:    500 * time.Millisecond,
			wantCalls:  1,
			wantAccess: "renewed",
		},
		{
			name:         "expired token rotation survives persistence beyond exchange timeout",
			operation:    "save failure",
			saveFailures: 6,
			body:         `{"access_token":"renewed","refresh_token":"rotated","expires_in":3600}`,
			expires:      -time.Hour,
			wantCalls:    1,
			wantAccess:   "renewed",
		},
		{
			name:       "expired token retains rotated credentials across save failures",
			operation:  "save failure",
			body:       `{"access_token":"renewed","refresh_token":"rotated","expires_in":3600}`,
			expires:    -time.Hour,
			wantCalls:  1,
			wantAccess: "renewed",
		},
		{
			name:       "retry saving rotated tokens without exchanging again",
			operation:  "save failure",
			body:       `{"access_token":"renewed","refresh_token":"rotated","expires_in":3600}`,
			expires:    time.Minute,
			wantCalls:  1,
			wantAccess: "renewed",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				synctest.Test(
					t,
					func(t *testing.T) {
						filesystem := &refreshFaultFS{
							Fs: afero.NewMemMapFs(),
						}
						store := NewStore(filesystem)
						previous := OAuthCredentials{
							AccessToken:  "original",
							RefreshToken: "refresh-secret",
							IDToken:      "original-id",
							AccountID:    "account",
							ExpiresAt:    time.Now().UTC().Add(test.expires),
						}
						for _, provider := range []Provider{
							OpenAI,
							OpenCode,
						} {
							if err := store.Save(
								t.Context(),
								"/auth.json",
								provider,
								Credentials{
									OpenAI: &previous,
									OpenCode: &APIKeyCredentials{
										APIKey: "other-key",
									},
								},
							); err != nil {
								t.Fatal(err)
							}
						}
						if test.operation == "save failure" {
							filesystem.failures = max(
								1,
								test.saveFailures,
							)
						}
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						calls := 0
						var issuedAt time.Time
						client := &http.Client{
							Transport: refreshTransport(func(request *http.Request) (*http.Response, error) {
								calls++
								if request.URL.String() != "https://auth.openai.com/oauth/token" || request.Method != http.MethodPost || request.ParseForm() != nil || request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != previous.RefreshToken || request.Form.Get("client_id") != openAIClientID {
									t.Error("incorrect refresh request")
								}
								switch test.operation {
								case "transport error":
									return nil, errors.New("refresh-secret secret-in-body")
								case "cancel":
									cancel()
									return nil, ctx.Err()
								case "timeout once":
									if calls == 1 {
										<-request.Context().Done()
										return nil, request.Context().Err()
									}
								case "logout", "login", "other provider":
									document := Credentials{}
									provider := OpenAI
									switch test.operation {
									case "login":
										tokens := previous
										tokens.AccessToken = "new-login"
										document.OpenAI = &tokens
									case "other provider":
										provider = OpenCode
										document.OpenCode = &APIKeyCredentials{
											APIKey: "changed-key",
										}
									}
									if err := store.Save(
										ctx,
										"/auth.json",
										provider,
										document,
									); err != nil {
										t.Error(err)
									}
								}
								status := http.StatusOK
								body := test.body
								if calls <= test.failures {
									status = http.StatusServiceUnavailable
									body = "refresh-secret secret-in-body"
								}
								if status == http.StatusOK {
									issuedAt = time.Now()
								}
								if test.operation == "redirect" {
									status = http.StatusTemporaryRedirect
								}
								return &http.Response{
									Header: http.Header{
										"Location": []string{
											"https://elsewhere.invalid/",
										},
									},
									StatusCode: status,
									Body:       io.NopCloser(strings.NewReader(body)),
								}, nil
							}),
						}
						result, err := store.RefreshOpenAI(
							ctx,
							"/auth.json",
							previous,
							client,
						)
						if (err != nil) != test.wantError || calls != test.wantCalls {
							t.Fatalf(
								"refresh: calls=%d, error=%v",
								calls,
								err,
							)
						}
						if err != nil && (strings.Contains(
							err.Error(),
							"refresh-secret",
						) || strings.Contains(
							err.Error(),
							"secret-in-body",
						)) {
							t.Fatal("refresh error leaked credentials")
						}
						stored, err := store.Retrieve(
							t.Context(),
							"/auth.json",
						)
						if err != nil {
							t.Fatal(err)
						}
						if test.wantAccess == "" {
							if stored.OpenAI != nil || result != nil {
								t.Fatal("refresh resurrected a logged-out credential")
							}
						} else if stored.OpenAI == nil || stored.OpenAI.AccessToken != test.wantAccess {
							t.Fatal("incorrect stored access token")
						}
						if test.wantAccess == "renewed" {
							if result == nil || *result != *stored.OpenAI || !result.ExpiresAt.Equal(issuedAt.Add(time.Hour)) {
								t.Fatal("refresh did not return durable credentials and updated expiry")
							}
							wantRefresh := previous.RefreshToken
							if strings.Contains(
								test.body,
								"rotated",
							) {
								wantRefresh = "rotated"
							}
							if result.RefreshToken != wantRefresh || result.AccountID != previous.AccountID {
								t.Fatal("rotation or account routing was lost")
							}
						}
						wantKey := "other-key"
						if test.operation == "other provider" {
							wantKey = "changed-key"
						}
						if stored.OpenCode == nil || stored.OpenCode.APIKey != wantKey {
							t.Fatal("refresh overwrote other provider credentials")
						}
					},
				)
			},
		)
	}
}

func TestConcurrentRefresh(t *testing.T) {
	for _, backend := range []string{
		"memory",
		"separate stores on disk",
	} {
		t.Run(
			backend,
			func(t *testing.T) {
				filesystem := afero.NewMemMapFs()
				path := "/auth.json"
				if backend == "separate stores on disk" {
					filesystem = afero.NewOsFs()
					path = filepath.Join(
						t.TempDir(),
						"auth.json",
					)
				}
				store := NewStore(filesystem)
				previous := OAuthCredentials{
					AccessToken:  "old",
					RefreshToken: "refresh",
					IDToken:      "id",
					AccountID:    "account",
					ExpiresAt:    time.Now().UTC().Add(time.Minute),
				}
				if err := store.Save(
					t.Context(),
					path,
					OpenAI,
					Credentials{
						OpenAI: &previous,
					},
				); err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				client := &http.Client{
					Transport: refreshTransport(func(*http.Request) (*http.Response, error) {
						calls.Add(1)
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(`{"access_token":"renewed","refresh_token":"rotated","expires_in":3600}`)),
						}, nil
					}),
				}
				results := make(
					chan error,
					8,
				)
				for range 8 {
					current := store
					if backend == "separate stores on disk" {
						current = NewStore(filesystem)
					}
					go func() {
						result, err := current.RefreshOpenAI(
							t.Context(),
							path,
							previous,
							client,
						)
						if err == nil && (result == nil || result.RefreshToken != "rotated") {
							err = errors.New("concurrent caller did not receive rotated token")
						}
						results <- err
					}()
				}
				for range 8 {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
				if calls.Load() != 1 {
					t.Fatal("concurrent refresh exchanged a rotating token more than once")
				}
			},
		)
	}
}
