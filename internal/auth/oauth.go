package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Default OAuth constants for Anthropic
const (
	defaultAnthropicClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultAnthropicAuthURL     = "https://claude.ai/oauth/authorize"
	defaultAnthropicTokenURL    = "https://console.anthropic.com/v1/oauth/token"
	defaultAnthropicRedirectURI = "https://console.anthropic.com/oauth/code/callback"
	defaultAnthropicScopes      = "org:create_api_key user:profile user:inference"

	AnthropicAuthBetaHeader = "oauth-2025-04-20,claude-code-20250219"
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
	EnvAnthropicClientID    = "CPE_ANTHROPIC_CLIENT_ID"
	EnvAnthropicAuthURL     = "CPE_ANTHROPIC_AUTH_URL"
	EnvAnthropicTokenURL    = "CPE_ANTHROPIC_TOKEN_URL"
	EnvAnthropicRedirectURI = "CPE_ANTHROPIC_REDIRECT_URI"
	EnvAnthropicScopes      = "CPE_ANTHROPIC_SCOPES"

	EnvOpenAIClientID    = "CPE_OPENAI_CLIENT_ID"
	EnvOpenAIAuthURL     = "CPE_OPENAI_AUTH_URL"
	EnvOpenAITokenURL    = "CPE_OPENAI_TOKEN_URL"
	EnvOpenAIRedirectURI = "CPE_OPENAI_REDIRECT_URI"
	EnvOpenAIScopes      = "CPE_OPENAI_SCOPES"
)

// getEnvOrDefault returns the environment variable value or the default if not set
func getEnvOrDefault(envVar, defaultVal string) string {
	if val := os.Getenv(envVar); val != "" {
		return val
	}
	return defaultVal
}

// GetAnthropicClientID returns the OAuth client ID from env var or default
func GetAnthropicClientID() string {
	return getEnvOrDefault(EnvAnthropicClientID, defaultAnthropicClientID)
}

// GetAnthropicAuthURL returns the OAuth authorization URL from env var or default
func GetAnthropicAuthURL() string {
	return getEnvOrDefault(EnvAnthropicAuthURL, defaultAnthropicAuthURL)
}

// GetAnthropicTokenURL returns the OAuth token URL from env var or default
func GetAnthropicTokenURL() string {
	return getEnvOrDefault(EnvAnthropicTokenURL, defaultAnthropicTokenURL)
}

// GetAnthropicRedirectURI returns the OAuth redirect URI from env var or default
func GetAnthropicRedirectURI() string {
	return getEnvOrDefault(EnvAnthropicRedirectURI, defaultAnthropicRedirectURI)
}

// GetAnthropicScopes returns the OAuth scopes from env var or default
func GetAnthropicScopes() string {
	return getEnvOrDefault(EnvAnthropicScopes, defaultAnthropicScopes)
}

// GetOpenAIClientID returns the OAuth client ID from env var or default
func GetOpenAIClientID() string {
	return getEnvOrDefault(EnvOpenAIClientID, defaultOpenAIClientID)
}

// GetOpenAIAuthURL returns the OAuth authorization URL from env var or default
func GetOpenAIAuthURL() string {
	return getEnvOrDefault(EnvOpenAIAuthURL, defaultOpenAIAuthURL)
}

// GetOpenAITokenURL returns the OAuth token URL from env var or default
func GetOpenAITokenURL() string {
	return getEnvOrDefault(EnvOpenAITokenURL, defaultOpenAITokenURL)
}

// GetOpenAIRedirectURI returns the OAuth redirect URI from env var or default
func GetOpenAIRedirectURI() string {
	return getEnvOrDefault(EnvOpenAIRedirectURI, defaultOpenAIRedirectURI)
}

// GetOpenAIScopes returns the OAuth scopes from env var or default
func GetOpenAIScopes() string {
	return getEnvOrDefault(EnvOpenAIScopes, defaultOpenAIScopes)
}

// ProviderOAuthConfig holds the OAuth configuration for a specific provider
type ProviderOAuthConfig struct {
	ClientID    string
	AuthURL     string
	TokenURL    string
	RedirectURI string
	Scopes      string
}

// GetProviderOAuthConfig returns the OAuth configuration for the given provider.
// Returns an error if the provider is not supported.
func GetProviderOAuthConfig(provider string) (ProviderOAuthConfig, error) {
	switch provider {
	case "anthropic":
		return ProviderOAuthConfig{
			ClientID:    GetAnthropicClientID(),
			AuthURL:     GetAnthropicAuthURL(),
			TokenURL:    GetAnthropicTokenURL(),
			RedirectURI: GetAnthropicRedirectURI(),
			Scopes:      GetAnthropicScopes(),
		}, nil
	case "openai":
		return ProviderOAuthConfig{
			ClientID:    GetOpenAIClientID(),
			AuthURL:     GetOpenAIAuthURL(),
			TokenURL:    GetOpenAITokenURL(),
			RedirectURI: GetOpenAIRedirectURI(),
			Scopes:      GetOpenAIScopes(),
		}, nil
	default:
		return ProviderOAuthConfig{}, fmt.Errorf("unsupported OAuth provider: %s", provider)
	}
}

// TokenResponse represents the OAuth token response
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
		"client_id":             {GetAnthropicClientID()},
		"response_type":         {"code"},
		"redirect_uri":          {GetAnthropicRedirectURI()},
		"scope":                 {GetAnthropicScopes()},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {verifier},
	}
	return GetAnthropicAuthURL() + "?" + params.Encode()
}

// BuildOpenAIAuthURL constructs the OAuth authorization URL for OpenAI with PKCE parameters.
// It uses a local callback server flow with a random state parameter.
func BuildOpenAIAuthURL(challenge, state string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {GetOpenAIClientID()},
		"redirect_uri":          {GetOpenAIRedirectURI()},
		"scope":                 {GetOpenAIScopes()},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return GetOpenAIAuthURL() + "?" + params.Encode()
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

// ExchangeCode exchanges an authorization code for tokens (Anthropic provider)
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
		"client_id":     GetAnthropicClientID(),
		"redirect_uri":  GetAnthropicRedirectURI(),
		"code_verifier": verifier,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GetAnthropicTokenURL(), bytes.NewReader(body))
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

// ExchangeCodeOpenAI exchanges an authorization code for tokens (OpenAI provider).
// OpenAI uses application/x-www-form-urlencoded for token exchange.
func ExchangeCodeOpenAI(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {GetOpenAIClientID()},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {GetOpenAIRedirectURI()},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GetOpenAITokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

// RefreshAccessToken uses the refresh token to get a new access token (Anthropic)
func RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     GetAnthropicClientID(),
		"redirect_uri":  GetAnthropicRedirectURI(),
		"refresh_token": refreshToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GetAnthropicTokenURL(), bytes.NewReader(body))
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

// RefreshAccessTokenOpenAI uses the refresh token to get a new access token (OpenAI).
// OpenAI uses application/x-www-form-urlencoded for token refresh.
func RefreshAccessTokenOpenAI(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {GetOpenAIClientID()},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GetOpenAITokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

// TokenToCredential converts a token response to a credential for the given provider
func TokenToCredential(provider string, token *TokenResponse) *Credential {
	return &Credential{
		Type:         "oauth",
		Provider:     provider,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().Unix() + int64(token.ExpiresIn),
	}
}

// DecodeJWTClaims extracts claims from a JWT token without verifying the signature.
// This is used to extract the chatgpt_account_id from the access token.
func DecodeJWTClaims(token string) (map[string]any, error) {
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

// ExtractChatGPTAccountID extracts the chatgpt_account_id from a JWT access token.
// The account ID is nested under "https://api.openai.com/auth" -> "chatgpt_account_id".
func ExtractChatGPTAccountID(accessToken string) (string, error) {
	claims, err := DecodeJWTClaims(accessToken)
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
