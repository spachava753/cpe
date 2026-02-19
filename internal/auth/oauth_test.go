package auth

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestGetProviderOAuthConfig(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
		wantURL  string // partial check on auth URL
	}{
		{
			name:     "anthropic",
			provider: "anthropic",
			wantURL:  "claude.ai",
		},
		{
			name:     "openai",
			provider: "openai",
			wantURL:  "auth.openai.com",
		},
		{
			name:     "unsupported",
			provider: "google",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := GetProviderOAuthConfig(tt.provider)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(cfg.AuthURL, tt.wantURL) {
				t.Errorf("AuthURL %q does not contain %q", cfg.AuthURL, tt.wantURL)
			}
			if cfg.ClientID == "" {
				t.Error("ClientID is empty")
			}
			if cfg.TokenURL == "" {
				t.Error("TokenURL is empty")
			}
			if cfg.RedirectURI == "" {
				t.Error("RedirectURI is empty")
			}
			if cfg.Scopes == "" {
				t.Error("Scopes is empty")
			}
		})
	}
}

func TestGetOpenAIDefaults(t *testing.T) {
	if got := GetOpenAIClientID(); got != "app_EMoamEEZ73f0CkXaXp7hrann" {
		t.Errorf("GetOpenAIClientID() = %q, want %q", got, "app_EMoamEEZ73f0CkXaXp7hrann")
	}
	if got := GetOpenAIAuthURL(); got != "https://auth.openai.com/oauth/authorize" {
		t.Errorf("GetOpenAIAuthURL() = %q, want %q", got, "https://auth.openai.com/oauth/authorize")
	}
	if got := GetOpenAITokenURL(); got != "https://auth.openai.com/oauth/token" {
		t.Errorf("GetOpenAITokenURL() = %q, want %q", got, "https://auth.openai.com/oauth/token")
	}
	if got := GetOpenAIRedirectURI(); got != "http://localhost:1455/auth/callback" {
		t.Errorf("GetOpenAIRedirectURI() = %q, want %q", got, "http://localhost:1455/auth/callback")
	}
	if got := GetOpenAIScopes(); got != "openid profile email offline_access" {
		t.Errorf("GetOpenAIScopes() = %q, want %q", got, "openid profile email offline_access")
	}
}

func TestGetOpenAIEnvOverrides(t *testing.T) {
	tests := []struct {
		envKey   string
		envVal   string
		getter   func() string
		expected string
	}{
		{EnvOpenAIClientID, "custom-client-id", GetOpenAIClientID, "custom-client-id"},
		{EnvOpenAIAuthURL, "https://custom.auth.com", GetOpenAIAuthURL, "https://custom.auth.com"},
		{EnvOpenAITokenURL, "https://custom.token.com", GetOpenAITokenURL, "https://custom.token.com"},
		{EnvOpenAIRedirectURI, "http://localhost:9999/cb", GetOpenAIRedirectURI, "http://localhost:9999/cb"},
		{EnvOpenAIScopes, "custom:scope", GetOpenAIScopes, "custom:scope"},
	}

	for _, tt := range tests {
		t.Run(tt.envKey, func(t *testing.T) {
			old := os.Getenv(tt.envKey)
			os.Setenv(tt.envKey, tt.envVal)
			defer os.Setenv(tt.envKey, old)

			if got := tt.getter(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestBuildOpenAIAuthURL(t *testing.T) {
	challenge := "test-challenge-abc"
	state := "test-state-123"

	authURL := BuildOpenAIAuthURL(challenge, state)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse auth URL: %v", err)
	}

	if parsed.Scheme != "https" {
		t.Errorf("scheme = %q, want 'https'", parsed.Scheme)
	}
	if parsed.Host != "auth.openai.com" {
		t.Errorf("host = %q, want 'auth.openai.com'", parsed.Host)
	}
	if parsed.Path != "/oauth/authorize" {
		t.Errorf("path = %q, want '/oauth/authorize'", parsed.Path)
	}

	q := parsed.Query()
	expectedParams := map[string]string{
		"response_type":         "code",
		"client_id":             "app_EMoamEEZ73f0CkXaXp7hrann",
		"redirect_uri":          "http://localhost:1455/auth/callback",
		"scope":                 "openid profile email offline_access",
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"state":                 state,
	}

	for key, want := range expectedParams {
		if got := q.Get(key); got != want {
			t.Errorf("param %q = %q, want %q", key, got, want)
		}
	}
}

func TestDecodeJWTClaims(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
		check   func(t *testing.T, claims map[string]any)
	}{
		{
			name:    "invalid - not a JWT",
			token:   "not-a-jwt",
			wantErr: true,
		},
		{
			name:    "invalid - only two parts",
			token:   "header.payload",
			wantErr: true,
		},
		{
			name:    "invalid - bad base64",
			token:   "header.!!!invalid!!!.signature",
			wantErr: true,
		},
		{
			name: "valid JWT with claims",
			// Header: {"alg":"HS256"}, Payload: {"sub":"1234","name":"Test"}, Signature: dummy
			token: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0IiwibmFtZSI6IlRlc3QifQ.signature",
			check: func(t *testing.T, claims map[string]any) {
				if claims["sub"] != "1234" {
					t.Errorf("sub = %v, want '1234'", claims["sub"])
				}
				if claims["name"] != "Test" {
					t.Errorf("name = %v, want 'Test'", claims["name"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := DecodeJWTClaims(tt.token)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, claims)
			}
		})
	}
}

func TestExtractChatGPTAccountID(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		wantID    string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "invalid JWT",
			token:   "not-a-jwt",
			wantErr: true,
		},
		{
			name: "missing auth claim",
			// Payload: {"sub":"1234"}
			token:     "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature",
			wantErr:   true,
			errSubstr: "missing",
		},
		{
			name: "valid with account ID",
			// Payload: {"https://api.openai.com/auth":{"chatgpt_account_id":"acc_123","user_id":"user_456"}}
			token:  "eyJhbGciOiJIUzI1NiJ9.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjXzEyMyIsInVzZXJfaWQiOiJ1c2VyXzQ1NiJ9fQ.signature",
			wantID: "acc_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ExtractChatGPTAccountID(tt.token)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("got %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestTokenToCredential(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{name: "anthropic", provider: "anthropic"},
		{name: "openai", provider: "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &TokenResponse{
				AccessToken:  "access-token-123",
				RefreshToken: "refresh-token-456",
				ExpiresIn:    3600,
			}

			cred := TokenToCredential(tt.provider, token)

			if cred.Type != "oauth" {
				t.Errorf("Type = %q, want 'oauth'", cred.Type)
			}
			if cred.Provider != tt.provider {
				t.Errorf("Provider = %q, want %q", cred.Provider, tt.provider)
			}
			if cred.AccessToken != "access-token-123" {
				t.Errorf("AccessToken = %q, want 'access-token-123'", cred.AccessToken)
			}
			if cred.RefreshToken != "refresh-token-456" {
				t.Errorf("RefreshToken = %q, want 'refresh-token-456'", cred.RefreshToken)
			}
			if cred.ExpiresAt == 0 {
				t.Error("ExpiresAt should be non-zero")
			}
		})
	}
}
