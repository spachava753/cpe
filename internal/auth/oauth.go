package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OAuth constants for Anthropic
const (
	AnthropicClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AnthropicAuthURL     = "https://claude.ai/oauth/authorize"
	AnthropicTokenURL    = "https://console.anthropic.com/v1/oauth/token"
	AnthropicRedirectURI = "https://console.anthropic.com/oauth/code/callback"
	AnthropicScopes      = "org:create_api_key user:profile user:inference"
	AnthropicBetaHeader  = "oauth-2025-04-20,claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"
)

// TokenResponse represents the OAuth token response from Anthropic
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// BuildAuthURL constructs the OAuth authorization URL with PKCE parameters
// Note: state is set to the verifier per Anthropic's OAuth implementation
func BuildAuthURL(challenge, verifier string) string {
	params := url.Values{
		"code":                  {"true"},
		"client_id":             {AnthropicClientID},
		"response_type":         {"code"},
		"redirect_uri":          {AnthropicRedirectURI},
		"scope":                 {AnthropicScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {verifier},
	}
	return AnthropicAuthURL + "?" + params.Encode()
}

// OpenBrowser opens the default browser to the given URL
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux and others
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}

// ExchangeCode exchanges an authorization code for tokens
// The code parameter should be in the format "code#state" as returned by the callback
func ExchangeCode(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	// Split code#state format
	authCode := code
	state := ""
	if idx := strings.Index(code, "#"); idx != -1 {
		authCode = code[:idx]
		state = code[idx+1:]
	}

	payload := map[string]string{
		"code":          authCode,
		"state":         state,
		"grant_type":    "authorization_code",
		"client_id":     AnthropicClientID,
		"redirect_uri":  AnthropicRedirectURI,
		"code_verifier": verifier,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", AnthropicTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("token exchange failed (status %d): %v", resp.StatusCode, errBody)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshAccessToken uses the refresh token to get a new access token
func RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     AnthropicClientID,
		"refresh_token": refreshToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", AnthropicTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("token refresh failed (status %d): %v", resp.StatusCode, errBody)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	return &tokenResp, nil
}

// TokenToCredential converts a token response to a credential
func TokenToCredential(token *TokenResponse) *Credential {
	return &Credential{
		Type:         "oauth",
		Provider:     "anthropic",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().Unix() + int64(token.ExpiresIn),
	}
}
