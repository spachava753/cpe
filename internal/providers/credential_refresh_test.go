package providers

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/models"
)

func refreshProviderFixture(t *testing.T, expiry time.Time, transport http.RoundTripper) (*Providers, *auth.Store) {
	t.Helper()
	store := auth.NewStore(afero.NewMemMapFs())
	if err := store.Save(
		t.Context(),
		"/auth.json",
		auth.OpenAI,
		auth.Credentials{
			OpenAI: &auth.OAuthCredentials{
				AccessToken:  "old-access",
				RefreshToken: "old-refresh",
				IDToken:      "id",
				AccountID:    "account",
				ExpiresAt:    expiry,
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	providers, err := New(
		store,
		"/auth.json",
		&http.Client{
			Transport: transport,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return providers, store
}

func refreshModel() models.Model {
	return models.Model{
		ID:  "fixture",
		API: models.Responses,
		BaseUrl: url.URL{
			Scheme: "https",
			Host:   "chatgpt.com",
			Path:   "/backend-api/codex/",
		},
	}
}

func refreshRequest(options gai.GenerationOptions) gai.GenerationRequest {
	return gai.GenerationRequest{
		Model: "fixture",
		Dialog: gai.Dialog{
			{
				Role: gai.User,
				Blocks: []gai.Block{
					gai.TextBlock("continue"),
				},
			},
		},
		Options: options,
	}
}

func TestCredentialRefreshRequests(t *testing.T) {
	for _, mode := range []string{
		"buffered",
		"streaming",
	} {
		t.Run(
			mode,
			func(t *testing.T) {
				synctest.Test(
					t,
					func(t *testing.T) {
						var attempts atomic.Int32
						var keys []string
						providers, store := refreshProviderFixture(
							t,
							time.Now().Add(10*time.Minute),
							roundTripFunc(func(request *http.Request) (*http.Response, error) {
								body := `{"id":"fixture","object":"response","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}]}`
								status := http.StatusOK
								contentType := "application/json"
								if request.URL.Host == "auth.openai.com" {
									if attempts.Add(1) < 3 {
										status = http.StatusServiceUnavailable
										body = "private-refresh-error"
									} else {
										body = `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`
									}
								} else {
									keys = append(
										keys,
										request.Header.Get("Authorization"),
									)
									if request.Header.Get("ChatGPT-Account-Id") != "account" || request.Header.Get("originator") != "cpe" {
										t.Error("refreshed request lost account routing")
									}
									if mode == "streaming" {
										contentType = "text/event-stream"
										body = "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
									}
								}
								return &http.Response{
									StatusCode: status,
									Header: http.Header{
										"Content-Type": []string{
											contentType,
										},
									},
									Body: io.NopCloser(strings.NewReader(body)),
								}, nil
							}),
						)
						defer providers.Close()
						generator, options, err := providers.Generator(
							t.Context(),
							refreshModel(),
							"",
						)
						if err != nil {
							t.Fatal(err)
						}
						invoke := func() {
							if mode == "streaming" {
								for chunk := range generator.(gai.StreamingGenerator).Stream(
									t.Context(),
									refreshRequest(options),
								) {
									if chunk.Err != nil {
										t.Fatal(chunk.Err)
									}
								}
							} else if _, err := generator.Generate(
								t.Context(),
								refreshRequest(options),
							); err != nil {
								t.Fatal(err)
							}
						}
						invoke()
						synctest.Wait()
						if attempts.Load() != 0 {
							t.Fatal("refreshed before the expiry window")
						}
						time.Sleep(6 * time.Minute)
						invoke()
						synctest.Wait()
						if attempts.Load() != 1 {
							t.Fatal("reused generator did not refresh near expiry")
						}
						time.Sleep(time.Second)
						synctest.Wait()
						invoke()
						if attempts.Load() != 2 {
							t.Fatal("refresh did not retry independently of model requests")
						}
						time.Sleep(2 * time.Second)
						synctest.Wait()
						invoke()
						if attempts.Load() != 3 || len(keys) != 4 || keys[0] != "Bearer old-access" || keys[1] != "Bearer old-access" || keys[2] != "Bearer old-access" || keys[3] != "Bearer new-access" {
							t.Fatal("requests did not continue with valid credentials and then switch to renewed credentials")
						}
						stored, err := store.Retrieve(
							t.Context(),
							"/auth.json",
						)
						if err != nil || stored.OpenAI == nil || stored.OpenAI.RefreshToken != "new-refresh" {
							t.Fatal("rotated refresh token was not persisted")
						}
					},
				)
			},
		)
	}
}

func TestCredentialRefreshLifetime(t *testing.T) {
	for _, operation := range []string{
		"expiry",
		"logout",
		"close",
		"turn cancellation",
	} {
		t.Run(
			operation,
			func(t *testing.T) {
				synctest.Test(
					t,
					func(t *testing.T) {
						var attempts atomic.Int32
						var modelCalls atomic.Int32
						release := make(chan struct{})
						providers, store := refreshProviderFixture(
							t,
							time.Now().Add(5*time.Second),
							roundTripFunc(func(request *http.Request) (*http.Response, error) {
								if request.URL.Host == "auth.openai.com" {
									attempts.Add(1)
									if operation == "expiry" {
										return &http.Response{
											StatusCode: http.StatusServiceUnavailable,
											Body:       io.NopCloser(strings.NewReader("private-auth-error")),
										}, nil
									}
									select {
									case <-request.Context().Done():
										return nil, request.Context().Err()
									case <-release:
										return &http.Response{
											StatusCode: http.StatusOK,
											Body:       io.NopCloser(strings.NewReader(`{"access_token":"new-access","expires_in":3600}`)),
										}, nil
									}
								}
								modelCalls.Add(1)
								return &http.Response{
									StatusCode: http.StatusOK,
									Header: http.Header{
										"Content-Type": []string{
											"application/json",
										},
									},
									Body: io.NopCloser(strings.NewReader(`{"id":"fixture","object":"response","status":"completed","output":[]}`)),
								}, nil
							}),
						)
						defer providers.Close()
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						generator, options, err := providers.Generator(
							ctx,
							refreshModel(),
							"",
						)
						if err != nil {
							t.Fatal(err)
						}
						synctest.Wait()
						if _, err := generator.Generate(
							ctx,
							refreshRequest(options),
						); err != nil {
							t.Fatal(err)
						}
						switch operation {
						case "expiry":
							time.Sleep(5 * time.Second)
							synctest.Wait()
							before := attempts.Load()
							time.Sleep(time.Minute)
							synctest.Wait()
							if before != 3 || attempts.Load() != before {
								t.Fatal("background refresh retried past access token expiry")
							}
						case "logout":
							if err := providers.Logout(t.Context()); err != nil {
								t.Fatal(err)
							}
						case "close":
							providers.Close()
						case "turn cancellation":
							cancel()
							synctest.Wait()
							close(release)
							synctest.Wait()
							stored, err := store.Retrieve(
								t.Context(),
								"/auth.json",
							)
							if err != nil || stored.OpenAI == nil || stored.OpenAI.AccessToken != "new-access" {
								t.Fatal("turn cancellation stopped connection-owned refresh")
							}
							return
						}
						_, err = generator.Generate(
							t.Context(),
							refreshRequest(options),
						)
						if err == nil || modelCalls.Load() != 1 || !IsCredentialError(err) || strings.Contains(
							err.Error(),
							"private-auth-error",
						) {
							t.Fatalf(
								"unavailable credentials reached the provider, became retryable, or leaked errors: %v",
								err,
							)
						}
					},
				)
			},
		)
	}
}
