package auth

import (
	"errors"
	"time"
	"unicode/utf8"
)

// Credentials contains the provider entries stored together in auth.json.
// Nil entries represent providers without saved credentials. All fields contain private account data.
type Credentials struct {
	OpenAI   *OAuthCredentials  `json:"openai,omitempty"`
	OpenCode *APIKeyCredentials `json:"opencode,omitempty"`
}

// OAuthCredentials holds ChatGPT subscription tokens and their account routing metadata.
type OAuthCredentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	AccountID    string    `json:"account_id"`
}

// APIKeyCredentials holds a provider-issued API key.
type APIKeyCredentials struct {
	APIKey string `json:"api_key"`
}

func (credentials Credentials) check() error {
	if credentials.OpenAI != nil {
		tokens := credentials.OpenAI
		if tokens.ExpiresAt.IsZero() {
			return errors.New("invalid OpenAI credentials")
		}
		for _, value := range []string{
			tokens.AccessToken,
			tokens.RefreshToken,
			tokens.IDToken,
			tokens.AccountID,
		} {
			if value == "" || !utf8.ValidString(value) {
				return errors.New("invalid OpenAI credentials")
			}
		}
	}
	if credentials.OpenCode != nil {
		if err := validateAPIKey(credentials.OpenCode.APIKey); err != nil {
			return errors.New("invalid OpenCode credentials")
		}
	}
	return nil
}
