package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/negrel/secrecy"
	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestProviders(t *testing.T) {
	for _, test := range []struct {
		name      string
		base      string
		api       models.API
		expired   bool
		explicit  bool
		wantKey   string
		wantError bool
	}{
		{
			name:    "ChatGPT subscription",
			base:    "https://chatgpt.com/backend-api/codex/",
			api:     models.Responses,
			wantKey: "access-fixture",
		},
		{
			name:      "expired subscription",
			base:      "https://chatgpt.com/backend-api/codex/",
			api:       models.Responses,
			expired:   true,
			wantError: true,
		},
		{
			name:    "OpenCode subscription",
			base:    "https://opencode.ai/zen/go/v1/",
			api:     models.Openai,
			wantKey: "opencode-fixture",
		},
		{
			name: "no token to lookalike host",
			base: "https://chatgpt.com.invalid/v1/",
			api:  models.Responses,
		},
		{
			name: "no token over HTTP",
			base: "http://chatgpt.com/backend-api/codex/",
			api:  models.Responses,
		},
		{
			name:      "no token to unrelated endpoint",
			base:      "https://example.invalid/v1/",
			api:       models.Openai,
			wantError: true,
		},
		{
			name:     "Anthropic ignores ambient credentials",
			base:     "https://proxy.invalid/",
			api:      models.Anthropic,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
		{
			name:     "explicit key takes precedence",
			base:     "https://opencode.ai/zen/go/v1/",
			api:      models.Openai,
			explicit: true,
			wantKey:  "explicit-fixture",
		},
	} {
		t.Run(
			test.name,
			func(t *testing.T) {
				if test.api == models.Anthropic {
					t.Setenv(
						"ANTHROPIC_AUTH_TOKEN",
						"ambient-fixture-must-not-be-sent",
					)
				}
				filesystem := afero.NewMemMapFs()
				store := auth.NewStore(filesystem)
				expires := time.Now().Add(time.Hour)
				if test.expired {
					expires = time.Now().Add(-time.Hour)
				}
				credentials := auth.Credentials{
					OpenAI: &auth.OAuthCredentials{
						AccessToken:  "access-fixture",
						RefreshToken: "refresh-fixture",
						IDToken:      "id-fixture",
						AccountID:    "account-fixture",
						ExpiresAt:    expires,
					},
					OpenCode: &auth.APIKeyCredentials{
						APIKey: "opencode-fixture",
					},
				}
				for _, provider := range []auth.Provider{
					auth.OpenAI,
					auth.OpenCode,
				} {
					if err := store.Save(
						t.Context(),
						"/auth.json",
						provider,
						credentials,
					); err != nil {
						t.Fatal(err)
					}
				}
				called := false
				providers, err := NewProviders(
					store,
					"/auth.json",
					&http.Client{
						Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
							called = true
							key := strings.TrimPrefix(
								request.Header.Get("Authorization"),
								"Bearer ",
							)
							if test.api == models.Anthropic {
								if request.Header.Get("Authorization") != "" {
									t.Error("ambient bearer token leaked to configured endpoint")
								}
								key = request.Header.Get("X-Api-Key")
							}
							if key != test.wantKey {
								t.Error("incorrect credential routing")
							}
							if test.wantKey == "access-fixture" && request.Header.Get("ChatGPT-Account-Id") != "account-fixture" {
								t.Error("missing account header")
							}
							var body map[string]any
							if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if test.api == models.Openai && body["reasoning_effort"] != "high" {
								t.Error("reasoning effort was not applied")
							}
							response := `{"id":"fixture","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
							if test.api == models.Anthropic {
								response = `{"id":"fixture","type":"message","role":"assistant","model":"fixture","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
							}
							if test.api == models.Responses {
								response = `{"id":"fixture","object":"response","status":"completed","output":[{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}]}`
							}
							return &http.Response{
								StatusCode: http.StatusOK,
								Header: http.Header{
									"Content-Type": []string{
										"application/json",
									},
								},
								Body: io.NopCloser(strings.NewReader(response)),
							}, nil
						}),
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				base, err := url.Parse(test.base)
				if err != nil {
					t.Fatal(err)
				}
				model := models.Model{
					ID:        "fixture",
					BaseUrl:   *base,
					API:       test.api,
					MaxTokens: 64,
				}
				if test.explicit {
					secret := secrecy.NewSecretString([]byte("explicit-fixture"))
					model.APIKey = &secret
				}
				generator, options, err := providers.Generator(
					t.Context(),
					model,
					"high",
				)
				if (err != nil) != test.wantError {
					t.Fatalf(
						"generator: %v",
						err,
					)
				}
				if test.wantError {
					if called {
						t.Fatal("HTTP called before generator configuration succeeded")
					}
					return
				}
				_, err = generator.Generate(
					t.Context(),
					gai.GenerationRequest{
						Model: "fixture",
						Dialog: gai.Dialog{
							{
								Role: gai.User,
								Blocks: []gai.Block{
									gai.TextBlock("hello"),
								},
							},
						},
						Options: options,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("provider did not use injected HTTP client")
				}
				if err := providers.Logout(context.Background()); err != nil {
					t.Fatal(err)
				}
				remaining, err := store.Retrieve(
					t.Context(),
					"/auth.json",
				)
				if err != nil || remaining.OpenAI != nil || remaining.OpenCode != nil {
					t.Fatal("logout did not clear local credentials")
				}
			},
		)
	}
}
