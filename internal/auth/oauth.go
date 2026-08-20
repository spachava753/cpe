package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/httpclient"
)

// Default OAuth constants for Anthropic
const (
	defaultAnthropicClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultAnthropicAuthURL     = "https://claude.ai/oauth/authorize"
	defaultAnthropicTokenURL    = "https://console.anthropic.com/v1/oauth/token"
	defaultAnthropicRedirectURI = "https://console.anthropic.com/oauth/code/callback"
	defaultAnthropicScopes      = "org:create_api_key user:profile user:inference"

	anthropicAuthBetaHeader = "oauth-2025-04-20,claude-code-20250219"
)

// Default OAuth constants for OpenAI
const (
	defaultOpenAIClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultOpenAIAuthURL     = "https://auth.openai.com/oauth/authorize"
	defaultOpenAITokenURL    = "https://auth.openai.com/oauth/token"
	defaultOpenAIRedirectURI = "http://localhost:1455/auth/callback"
	defaultOpenAIScopes      = "openid profile email offline_access"

	// OpenAICodexBaseURL is the base URL for OpenAI Codex API calls via OAuth.
	OpenAICodexBaseURL = "https://chatgpt.com/backend-api/codex"
)

// Environment variable names for OAuth configuration
const (
	envAnthropicClientID    = "CPE_ANTHROPIC_CLIENT_ID"
	envAnthropicAuthURL     = "CPE_ANTHROPIC_AUTH_URL"
	envAnthropicTokenURL    = "CPE_ANTHROPIC_TOKEN_URL"
	envAnthropicRedirectURI = "CPE_ANTHROPIC_REDIRECT_URI"
	envAnthropicScopes      = "CPE_ANTHROPIC_SCOPES"

	envOpenAIClientID    = "CPE_OPENAI_CLIENT_ID"
	envOpenAIAuthURL     = "CPE_OPENAI_AUTH_URL"
	envOpenAITokenURL    = "CPE_OPENAI_TOKEN_URL"
	envOpenAIRedirectURI = "CPE_OPENAI_REDIRECT_URI"
	envOpenAIScopes      = "CPE_OPENAI_SCOPES"
)

// getEnvOrDefault returns the environment variable value or the default if not set
func getEnvOrDefault(envVar, defaultVal string) string {
	if val := os.Getenv(envVar); val != "" {
		return val
	}
	return defaultVal
}

// getAnthropicClientID returns the OAuth client ID from env var or default
func getAnthropicClientID() string {
	return getEnvOrDefault(envAnthropicClientID, defaultAnthropicClientID)
}

// getAnthropicAuthURL returns the OAuth authorization URL from env var or default
func getAnthropicAuthURL() string {
	return getEnvOrDefault(envAnthropicAuthURL, defaultAnthropicAuthURL)
}

// getAnthropicTokenURL returns the OAuth token URL from env var or default
func getAnthropicTokenURL() string {
	return getEnvOrDefault(envAnthropicTokenURL, defaultAnthropicTokenURL)
}

// getAnthropicRedirectURI returns the OAuth redirect URI from env var or default
func getAnthropicRedirectURI() string {
	return getEnvOrDefault(envAnthropicRedirectURI, defaultAnthropicRedirectURI)
}

// getAnthropicScopes returns the OAuth scopes from env var or default
func getAnthropicScopes() string {
	return getEnvOrDefault(envAnthropicScopes, defaultAnthropicScopes)
}

// getOpenAIClientID returns the OAuth client ID from env var or default
func getOpenAIClientID() string {
	return getEnvOrDefault(envOpenAIClientID, defaultOpenAIClientID)
}

// getOpenAIAuthURL returns the OAuth authorization URL from env var or default
func getOpenAIAuthURL() string {
	return getEnvOrDefault(envOpenAIAuthURL, defaultOpenAIAuthURL)
}

// getOpenAITokenURL returns the OAuth token URL from env var or default
func getOpenAITokenURL() string {
	return getEnvOrDefault(envOpenAITokenURL, defaultOpenAITokenURL)
}

// getOpenAIRedirectURI returns the OAuth redirect URI from env var or default
func getOpenAIRedirectURI() string {
	return getEnvOrDefault(envOpenAIRedirectURI, defaultOpenAIRedirectURI)
}

// getOpenAIScopes returns the OAuth scopes from env var or default
func getOpenAIScopes() string {
	return getEnvOrDefault(envOpenAIScopes, defaultOpenAIScopes)
}

// providerOAuthConfig holds the OAuth configuration for a specific provider
type providerOAuthConfig struct {
	ClientID    string
	AuthURL     string
	TokenURL    string
	RedirectURI string
	Scopes      string
}

// getProviderOAuthConfig returns the OAuth configuration for the given provider.
// Returns an error if the provider is not supported.
func getProviderOAuthConfig(provider string) (providerOAuthConfig, error) {
	switch provider {
	case "anthropic":
		return providerOAuthConfig{
			ClientID:    getAnthropicClientID(),
			AuthURL:     getAnthropicAuthURL(),
			TokenURL:    getAnthropicTokenURL(),
			RedirectURI: getAnthropicRedirectURI(),
			Scopes:      getAnthropicScopes(),
		}, nil
	case "openai":
		return providerOAuthConfig{
			ClientID:    getOpenAIClientID(),
			AuthURL:     getOpenAIAuthURL(),
			TokenURL:    getOpenAITokenURL(),
			RedirectURI: getOpenAIRedirectURI(),
			Scopes:      getOpenAIScopes(),
		}, nil
	default:
		return providerOAuthConfig{}, fmt.Errorf("unsupported OAuth provider: %s", provider)
	}
}

// tokenResponse represents the OAuth token response
type tokenResponse struct {
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
		"client_id":             {getAnthropicClientID()},
		"response_type":         {"code"},
		"redirect_uri":          {getAnthropicRedirectURI()},
		"scope":                 {getAnthropicScopes()},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {verifier},
	}
	return getAnthropicAuthURL() + "?" + params.Encode()
}

// BuildOpenAIAuthURL constructs the OAuth authorization URL for OpenAI with PKCE parameters.
// It uses a local callback server flow with a random state parameter.
func BuildOpenAIAuthURL(challenge, state string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {getOpenAIClientID()},
		"redirect_uri":          {getOpenAIRedirectURI()},
		"scope":                 {getOpenAIScopes()},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return getOpenAIAuthURL() + "?" + params.Encode()
}

// OpenBrowser opens the default browser to the given URL
func OpenBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux and others
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}

	return cmd.Start()
}

func newOAuthHTTPClient(maxRetries int) *http.Client {
	return httpclient.New(
		httpclient.WithRetryStatuses(false),
		httpclient.WithBackoff(200*time.Millisecond, 2*time.Second),
		httpclient.WithJitterFactor(0.2),
		httpclient.WithMaxRetries(maxRetries),
		httpclient.WithTimeout(30*time.Second),
	)
}

func oauthResponseError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		return fmt.Errorf("%s failed (status %d)", action, resp.StatusCode)
	}
	return fmt.Errorf("%s failed (status %d): %s", action, resp.StatusCode, bodyText)
}

// ExchangeCode exchanges an authorization code for tokens (Anthropic provider)
// The code parameter should be in the format "code#state" as returned by the callback
func ExchangeCode(ctx context.Context, code, verifier string) (*tokenResponse, error) {
	// Split code#state format
	authCode := code
	state := ""
	if before, after, ok := strings.Cut(code, "#"); ok {
		authCode = before
		state = after
	}

	payload := map[string]string{
		"code":          authCode,
		"state":         state,
		"grant_type":    "authorization_code",
		"client_id":     getAnthropicClientID(),
		"redirect_uri":  getAnthropicRedirectURI(),
		"code_verifier": verifier,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", getAnthropicTokenURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := newOAuthHTTPClient(1).Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, oauthResponseError("token exchange", resp)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	return &tokenResp, nil
}

// ExchangeCodeOpenAI exchanges an authorization code for tokens (OpenAI provider).
// OpenAI uses application/x-www-form-urlencoded for token exchange.
func ExchangeCodeOpenAI(ctx context.Context, code, verifier string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {getOpenAIClientID()},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {getOpenAIRedirectURI()},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", getOpenAITokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := newOAuthHTTPClient(1).Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, oauthResponseError("token exchange", resp)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	return &tokenResp, nil
}

// refreshAccessToken uses the refresh token to get a new access token (Anthropic)
func refreshAccessToken(ctx context.Context, refreshToken string) (*tokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     getAnthropicClientID(),
		"redirect_uri":  getAnthropicRedirectURI(),
		"refresh_token": refreshToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", getAnthropicTokenURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := newOAuthHTTPClient(2).Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, oauthResponseError("token refresh", resp)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshAccessTokenOpenAI uses the refresh token to get a new access token (OpenAI).
// OpenAI uses application/x-www-form-urlencoded for token refresh.
func RefreshAccessTokenOpenAI(ctx context.Context, refreshToken string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {getOpenAIClientID()},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", getOpenAITokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := newOAuthHTTPClient(2).Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, oauthResponseError("token refresh", resp)
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	return &tokenResp, nil
}

// TokenToCredential converts a token response to a credential for the given provider.
func TokenToCredential(provider string, token *tokenResponse) *Credential {
	return TokenToCredentialPreserveRefresh(provider, token, "")
}

// TokenToCredentialPreserveRefresh converts a token response to a credential for
// the given provider, preserving the existing refresh token when the token
// response omits a new one.
func TokenToCredentialPreserveRefresh(provider string, token *tokenResponse, existingRefreshToken string) *Credential {
	refreshToken := token.RefreshToken
	if refreshToken == "" {
		refreshToken = existingRefreshToken
	}
	return &Credential{
		Type:         "oauth",
		Provider:     provider,
		AccessToken:  token.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Unix() + int64(token.ExpiresIn),
	}
}

// decodeJWTClaims extracts claims from a JWT token without verifying the signature.
// This is used to extract the chatgpt_account_id from the access token.
func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT: expected 3 parts, got %d", len(parts))
	}

	// JWT uses raw base64url encoding (no padding)
	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decoding JWT payload: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("parsing JWT claims: %w", err)
	}

	return claims, nil
}

// extractChatGPTAccountID extracts the chatgpt_account_id from a JWT access token.
// The account ID is nested under "https://api.openai.com/auth" -> "chatgpt_account_id".
func extractChatGPTAccountID(accessToken string) (string, error) {
	claims, err := decodeJWTClaims(accessToken)
	if err != nil {
		return "", fmt.Errorf("decoding JWT: %w", err)
	}

	authClaims, ok := claims["https://api.openai.com/auth"]
	if !ok {
		return "", fmt.Errorf("JWT missing 'https://api.openai.com/auth' claim")
	}

	authMap, ok := authClaims.(map[string]any)
	if !ok {
		return "", fmt.Errorf("JWT auth claim is not an object")
	}

	accountID, ok := authMap["chatgpt_account_id"]
	if !ok {
		return "", fmt.Errorf("JWT auth claim missing 'chatgpt_account_id'")
	}

	accountIDStr, ok := accountID.(string)
	if !ok {
		return "", fmt.Errorf("chatgpt_account_id is not a string")
	}

	return accountIDStr, nil
}
