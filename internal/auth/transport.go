package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OAuthTransport wraps an http.RoundTripper to inject OAuth bearer tokens
type OAuthTransport struct {
	base  http.RoundTripper
	store *Store
	mu    sync.RWMutex
}

// NewOAuthTransport creates a new OAuth transport wrapper.
// If base is nil, http.DefaultTransport is used.
func NewOAuthTransport(base http.RoundTripper, store *Store) *OAuthTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &OAuthTransport{
		base:  base,
		store: store,
	}
}

// NewOAuthHTTPClient creates an HTTP client configured for OAuth authentication.
// If base is nil, http.DefaultTransport is used as the underlying transport.
func NewOAuthHTTPClient(base http.RoundTripper, store *Store) *http.Client {
	return &http.Client{
		Transport: NewOAuthTransport(base, store),
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

	// Merge beta headers - preserve any existing betas and add OAuth required ones
	existingBeta := clone.Header.Get("anthropic-beta")
	mergedBeta := mergeBetaHeaders(existingBeta, AnthropicAuthBetaHeader)

	// Set Bearer auth and required headers
	clone.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	clone.Header.Set("anthropic-beta", mergedBeta)
	clone.Header.Del("x-api-key")

	return t.base.RoundTrip(clone)
}

// mergeBetaHeaders combines existing and required beta headers, deduplicating
func mergeBetaHeaders(existing, required string) string {
	seen := make(map[string]bool)
	var result []string

	// Add required headers first
	for h := range strings.SplitSeq(required, ",") {
		h = strings.TrimSpace(h)
		if h != "" && !seen[h] {
			seen[h] = true
			result = append(result, h)
		}
	}

	// Add existing headers
	for h := range strings.SplitSeq(existing, ",") {
		h = strings.TrimSpace(h)
		if h != "" && !seen[h] {
			seen[h] = true
			result = append(result, h)
		}
	}

	return strings.Join(result, ",")
}
