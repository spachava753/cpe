package agent

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/spachava753/gai"
	"github.com/spf13/afero"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/models"
	"github.com/spachava753/cpe/internal/providers"
)

// Construct a real adapter error without sending a request or using host credentials.
func credentialFailure(t *testing.T) error {
	t.Helper()
	store := auth.NewStore(afero.NewMemMapFs())
	if err := store.Save(
		t.Context(),
		"/auth.json",
		auth.OpenAI,
		auth.Credentials{
			OpenAI: &auth.OAuthCredentials{
				AccessToken:  "fixture",
				RefreshToken: "fixture-refresh",
				IDToken:      "fixture-id",
				AccountID:    "fixture-account",
				ExpiresAt:    time.Now().Add(time.Hour),
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	providerSet, providerErr := providers.New(
		store,
		"/auth.json",
		&http.Client{},
	)
	if providerErr != nil {
		t.Fatal(providerErr)
	}
	defer providerSet.Close()
	generator, options, providerErr := providerSet.Generator(
		t.Context(),
		"conversation-fixture",
		models.Model{
			ID:  "fixture",
			API: models.Responses,
			BaseURL: url.URL{
				Scheme: "https",
				Host:   "chatgpt.com",
				Path:   "/backend-api/codex/",
			},
		},
		"",
	)
	if providerErr != nil {
		t.Fatal(providerErr)
	}
	if err := providerSet.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, providerErr = generator.Generate(
		t.Context(),
		gai.GenerationRequest{
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
		},
	)
	if !providers.IsCredentialError(providerErr) {
		t.Fatalf(
			"expected adapter credential failure, got %v",
			providerErr,
		)
	}
	return providerErr
}
