package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/commandio"
)

func TestDeviceResponse(t *testing.T) {
	for _, test := range []struct {
		name     string
		id       string
		code     string
		interval string
		want     time.Duration
		wantErr  bool
	}{
		{
			name:     "string interval",
			id:       "id",
			code:     "ABCD-EFGH",
			interval: `"5"`,
			want:     5 * time.Second,
		},
		{
			name:     "number interval",
			id:       "id",
			code:     "ABCD-EFGH",
			interval: `2`,
			want:     2 * time.Second,
		},
		{
			name: "default interval",
			id:   "id",
			code: "ABCD-EFGH",
			want: 5 * time.Second,
		},
		{
			name:    "missing ID",
			code:    "ABCD-EFGH",
			wantErr: true,
		},
		{
			name:    "missing code",
			id:      "id",
			wantErr: true,
		},
		{
			name:    "terminal escape",
			id:      "id",
			code:    "\x1bsecret",
			wantErr: true,
		},
		{
			name:     "null",
			id:       "id",
			code:     "code",
			interval: `null`,
			wantErr:  true,
		},
		{
			name:     "negative",
			id:       "id",
			code:     "code",
			interval: `-1`,
			wantErr:  true,
		},
		{
			name:     "zero",
			id:       "id",
			code:     "code",
			interval: `0`,
			wantErr:  true,
		},
		{
			name:     "overflow",
			id:       "id",
			code:     "code",
			interval: `9223372036854775807`,
			wantErr:  true,
		},
		{
			name:     "fraction",
			id:       "id",
			code:     "code",
			interval: `1.5`,
			wantErr:  true,
		},
		{
			name:     "noninteger string",
			id:       "id",
			code:     "code",
			interval: `"later"`,
			wantErr:  true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				issued := deviceResponse{
					DeviceAuthID: test.id,
					UserCode:     test.code,
					Interval:     json.RawMessage(test.interval),
				}
				code, err := issued.parse()
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"error: %v",
						err,
					)
				}
				if !test.wantErr && (code.id != test.id || code.userCode != test.code || code.interval != test.want) {
					t.Error("incorrect device code")
				}
			},
		)
	}
}

func TestPollDevice(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []int
		body     string
		cancel   bool
		wantErr  bool
	}{
		{
			name: "ready",
			statuses: []int{
				200,
			},
			body: `{"authorization_code":"code","code_verifier":"verifier"}`,
		},
		{
			name: "pending",
			statuses: []int{
				403,
				404,
				200,
			},
			body: `{"authorization_code":"code","code_verifier":"verifier"}`,
		},
		{
			name: "incomplete",
			statuses: []int{
				200,
			},
			body:    `{"authorization_code":"code"}`,
			wantErr: true,
		},
		{
			name: "malformed",
			statuses: []int{
				200,
			},
			body:    "secret-body",
			wantErr: true,
		},
		{
			name: "server error",
			statuses: []int{
				500,
			},
			body:    "secret-body",
			wantErr: true,
		},
		{
			name: "rate limit",
			statuses: []int{
				429,
			},
			body:    "secret-body",
			wantErr: true,
		},
		{
			name: "cancel pending",
			statuses: []int{
				403,
			},
			cancel:  true,
			wantErr: true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					5*time.Second,
				)
				defer cancel()
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					var values map[string]string
					if err := json.NewDecoder(request.Body).Decode(&values); err != nil {
						t.Error(err)
					}
					if values["device_auth_id"] != "id" || values["user_code"] != "user-code" {
						t.Error("wrong poll parameters")
					}
					index := int(calls.Add(1)) - 1
					if index >= len(test.statuses) {
						t.Error("too many polls")
						writer.WriteHeader(500)
						return
					}
					writer.WriteHeader(test.statuses[index])
					_, _ = io.WriteString(
						writer,
						test.body,
					)
					if test.cancel {
						cancel()
					}
				}))
				defer server.Close()
				client := openAIClient{
					client: server.Client(),
					issuer: server.URL,
				}
				grant, err := client.pollDevice(
					ctx,
					deviceCode{
						id:       "id",
						userCode: "user-code",
						interval: time.Millisecond,
					},
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"error: %v",
						err,
					)
				}
				if test.cancel && !errors.Is(
					err,
					context.Canceled,
				) {
					t.Errorf(
						"cancel error: %v",
						err,
					)
				}
				if !test.wantErr && (grant.Code != "code" || grant.Verifier != "verifier") {
					t.Error("wrong authorization")
				}
				if err != nil && strings.Contains(
					err.Error(),
					"secret-body",
				) {
					t.Error("error exposed authorization data")
				}
				if !test.cancel && calls.Load() != int32(len(test.statuses)) {
					t.Error("incorrect poll count")
				}
			},
		)
	}
}

func TestDeviceLogin(t *testing.T) {
	for _, test := range []struct {
		name        string
		issueStatus int
		tokenStatus int
		wantErr     bool
	}{
		{
			name:        "authorize and save",
			issueStatus: 200,
			tokenStatus: 200,
		},
		{
			name:        "device disabled",
			issueStatus: 404,
			tokenStatus: 200,
			wantErr:     true,
		},
		{
			name:        "exchange failed",
			issueStatus: 200,
			tokenStatus: 401,
			wantErr:     true,
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					switch request.URL.Path {
					case "/api/accounts/deviceauth/usercode":
						var body map[string]string
						if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if body["client_id"] != openAIClientID {
							t.Error("wrong client ID")
						}
						writer.WriteHeader(test.issueStatus)
						_, _ = io.WriteString(
							writer,
							`{"device_auth_id":"id","user_code":"ABCD-EFGH","interval":"5"}`,
						)
					case "/api/accounts/deviceauth/token":
						_, _ = io.WriteString(
							writer,
							`{"authorization_code":"authorization-secret","code_verifier":"verifier-secret"}`,
						)
					case "/oauth/token":
						if err := request.ParseForm(); err != nil {
							t.Error(err)
						}
						if !strings.HasSuffix(
							request.PostForm.Get("redirect_uri"),
							"/deviceauth/callback",
						) || request.PostForm.Get("code_verifier") != "verifier-secret" {
							t.Error("wrong device token exchange")
						}
						writer.WriteHeader(test.tokenStatus)
						_, _ = fmt.Fprintf(
							writer,
							`{"access_token":"access-secret","refresh_token":"refresh-secret","id_token":"%s","expires_in":3600}`,
							testToken("account"),
						)
					default:
						t.Error("unexpected endpoint")
						writer.WriteHeader(404)
					}
				}))
				defer server.Close()
				directory := filepath.Join(
					t.TempDir(),
					"auth",
				)
				filesystem := afero.NewMemMapFs()
				store := NewStore(filesystem)
				path := filepath.Join(
					directory,
					credentialFile,
				)
				var output, diagnostics bytes.Buffer
				err := login(
					context.Background(),
					store,
					LoginOptions{
						Provider: OpenAI,
						Path:     path,
						Device:   true,
					},
					commandio.IO{
						Out: &output,
						Err: &diagnostics,
					},
					loginFlow{
						oauth: openAIClient{
							client: server.Client(),
							issuer: server.URL,
						},
					},
				)
				if (err != nil) != test.wantErr {
					t.Fatalf(
						"error: %v",
						err,
					)
				}
				for _, secret := range []string{
					"access-secret",
					"refresh-secret",
					"authorization-secret",
					"verifier-secret",
				} {
					if strings.Contains(
						diagnostics.String()+output.String(),
						secret,
					) {
						t.Error("printed credential")
					}
				}
				if test.wantErr {
					if _, err := filesystem.Stat(directory); !os.IsNotExist(err) {
						t.Error("failed login saved state")
					}
					return
				}
				if !strings.Contains(
					diagnostics.String(),
					"ABCD-EFGH",
				) || !strings.Contains(
					diagnostics.String(),
					"/codex/device",
				) {
					t.Error("missing device instructions")
				}
				data, err := afero.ReadFile(
					filesystem,
					path,
				)
				if err != nil {
					t.Fatal(err)
				}
				var document Credentials
				if err := json.Unmarshal(
					data,
					&document,
				); err != nil {
					t.Fatal(err)
				}
				if document.OpenAI == nil || document.OpenAI.AccountID != "account" || document.OpenAI.AccessToken != "access-secret" {
					t.Error("wrong saved tokens")
				}
			},
		)
	}
}
