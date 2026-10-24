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
	providerSet, err := providers.New(
		store,
		"/auth.json",
		&http.Client{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer providerSet.Close()
	generator, options, err := providerSet.Generator(
		t.Context(),
		"conversation-fixture",
		models.Model{
			ID:  "fixture",
			API: models.Responses,
			BaseUrl: url.URL{
				Scheme: "https",
				Host:   "chatgpt.com",
				Path:   "/backend-api/codex/",
			},
		},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := providerSet.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(
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
	if !providers.IsCredentialError(err) {
		t.Fatalf(
			"expected adapter credential failure, got %v",
			err,
		)
	}
	return err
}
