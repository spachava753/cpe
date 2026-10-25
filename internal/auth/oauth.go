package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// TODO: replace with sign in with chatgpt
// This public client is also used by Codex and OpenCode's ChatGPT integration.
const openAIClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
const clientIDParameter = "client_id"
const loginTimeout = 15 * time.Minute
const maxAuthResponse = 1024 * 1024

type openAIClient struct {
	client  *http.Client
	issuer  string
	address string
}

type tokenResponse struct {
	ExpiresIn    *int64 `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

func (client openAIClient) exchange(ctx context.Context, code, verifier, redirect string) (OAuthCredentials, error) {
	if code == "" || verifier == "" {
		return OAuthCredentials{}, errors.New("OpenAI returned incomplete authorization")
	}
	values := url.Values{
		"grant_type": {
			"authorization_code",
		},
		clientIDParameter: {
			openAIClientID,
		},
		"code": {
			code,
		},
		"code_verifier": {
			verifier,
		},
		"redirect_uri": {
			redirect,
		},
	}
	response, err := client.post(
		ctx,
		"/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return OAuthCredentials{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return OAuthCredentials{}, fmt.Errorf(
			"OpenAI token exchange failed (HTTP %d)",
			response.StatusCode,
		)
	}
	var tokens tokenResponse
	if err := decodeAuthResponse(
		response.Body,
		&tokens,
	); err != nil {
		return OAuthCredentials{}, err
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" {
		return OAuthCredentials{}, errors.New("OpenAI returned incomplete tokens")
	}
	account := accountID(tokens.IDToken)
	if account == "" {
		account = accountID(tokens.AccessToken)
	}
	if account == "" {
		return OAuthCredentials{}, errors.New("OpenAI tokens are missing the ChatGPT account ID")
	}
	expires := int64(3600)
	if tokens.ExpiresIn != nil {
		expires = *tokens.ExpiresIn
	}
	if expires <= 0 || expires > int64((365*24*time.Hour)/time.Second) {
		return OAuthCredentials{}, errors.New("OpenAI returned an invalid token lifetime")
	}
	return OAuthCredentials{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		IDToken:      tokens.IDToken,
		ExpiresAt:    time.Now().UTC().Add(time.Duration(expires) * time.Second),
		AccountID:    account,
	}, nil
}

func (client openAIClient) post(ctx context.Context, path, contentType string, body io.Reader) (*http.Response, error) {
	if client.client == nil || client.issuer == "" {
		return nil, errors.New("OpenAI authentication requires an HTTP client and issuer")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.issuer+path,
		body,
	)
	if err != nil {
		return nil, errors.New("create OpenAI authentication request")
	}
	request.Header.Set(
		"Content-Type",
		contentType,
	)
	request.Header.Set(
		"Accept",
		"application/json",
	)
	response, err := client.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Transport and backend errors may include credentials; keep them private.
		return nil, errors.New("contact OpenAI authentication service")
	}
	return response, nil
}

func decodeAuthResponse(reader io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(
		reader,
		maxAuthResponse+1,
	))
	if err != nil {
		return errors.New("read OpenAI authentication response")
	}
	defer clear(data)
	if len(data) > maxAuthResponse || !utf8.Valid(data) || bytes.Equal(
		bytes.TrimSpace(data),
		[]byte("null"),
	) {
		return errors.New("invalid OpenAI authentication response")
	}
	if err := json.Unmarshal(
		data,
		value,
	); err != nil {
		return errors.New("invalid OpenAI authentication response")
	}
	return nil
}

// accountID reads routing metadata from tokens obtained directly from the issuer.
// It does not verify the JWT or use its claims as an authorization decision.
func accountID(token string) string {
	parts := strings.Split(
		token,
		".",
	)
	if len(parts) != 3 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		AccountID string `json:"chatgpt_account_id"`
		Auth      struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(
		data,
		&claims,
	); err != nil {
		return ""
	}
	if claims.Auth.AccountID != "" {
		return claims.Auth.AccountID
	}
	return claims.AccountID
}
