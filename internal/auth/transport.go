package auth

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// OAuthTransport wraps an http.RoundTripper to inject OAuth bearer tokens
type OAuthTransport struct {
	base  http.RoundTripper
	store *Store
	mu    sync.RWMutex
}

// NewOAuthTransport creates a new OAuth transport wrapper
func NewOAuthTransport(store *Store) *OAuthTransport {
	return &OAuthTransport{
		base:  http.DefaultTransport,
		store: store,
	}
}

// NewOAuthHTTPClient creates an HTTP client configured for OAuth authentication
func NewOAuthHTTPClient(store *Store) *http.Client {
	return &http.Client{
		Transport: NewOAuthTransport(store),
		Timeout:   5 * time.Minute,
	}
}

// RoundTrip implements http.RoundTripper
func (t *OAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cred, err := t.store.GetCredential("anthropic")
	if err != nil {
		return nil, fmt.Errorf("getting credential: %w", err)
	}

	// Check if token needs refresh (with 60 second buffer)
	if cred.ExpiresAt > 0 && time.Now().Unix() >= cred.ExpiresAt-60 {
		t.mu.Lock()
		// Double-check after acquiring lock
		cred, err = t.store.GetCredential("anthropic")
		if err != nil {
			t.mu.Unlock()
			return nil, fmt.Errorf("getting credential: %w", err)
		}

		if time.Now().Unix() >= cred.ExpiresAt-60 {
			tokenResp, err := RefreshAccessToken(context.Background(), cred.RefreshToken)
			if err != nil {
				t.mu.Unlock()
				return nil, fmt.Errorf("refreshing token: %w", err)
			}

			cred = TokenToCredential(tokenResp)
			if err := t.store.SaveCredential(cred); err != nil {
				t.mu.Unlock()
				return nil, fmt.Errorf("saving refreshed credential: %w", err)
			}
		}
		t.mu.Unlock()
	}

	// Clone the request to avoid modifying the original
	clone := req.Clone(req.Context())

	// Set Bearer auth and required headers
	clone.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	clone.Header.Set("anthropic-beta", AnthropicBetaHeader)
	clone.Header.Del("x-api-key")

	return t.base.RoundTrip(clone)
}
