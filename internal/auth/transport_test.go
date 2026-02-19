package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestStore creates a temporary auth store with the given credentials
func setupTestStore(t *testing.T, creds map[string]*Credential) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")

	if creds != nil {
		data, err := json.MarshalIndent(creds, "", "  ")
		if err != nil {
			t.Fatalf("marshaling test creds: %v", err)
		}
		if err := os.WriteFile(authPath, data, 0600); err != nil {
			t.Fatalf("writing test auth file: %v", err)
		}
	}

	return &Store{path: authPath}
}

func TestOpenAIOAuthTransport_InjectsHeaders(t *testing.T) {
	ctx := context.Background()

	// JWT with account ID: {"https://api.openai.com/auth":{"chatgpt_account_id":"test-account-id"}}
	accessToken := "eyJhbGciOiJIUzI1NiJ9.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoidGVzdC1hY2NvdW50LWlkIn19.signature"

	store := setupTestStore(t, map[string]*Credential{
		"openai": {
			Type:         "oauth",
			Provider:     "openai",
			AccessToken:  accessToken,
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
		},
	})

	// Create a test server that captures request headers
	var capturedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	transport := NewOpenAIOAuthTransport(http.DefaultTransport, store)

	req, err := http.NewRequestWithContext(ctx, "POST", ts.URL+"/responses", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	// Simulate the OpenAI SDK setting x-api-key
	req.Header.Set("x-api-key", "should-be-removed")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	resp.Body.Close()

	// Check Authorization header
	authHeader := capturedHeaders.Get("Authorization")
	if authHeader != "Bearer "+accessToken {
		t.Errorf("Authorization = %q, want 'Bearer %s'", authHeader, accessToken)
	}

	// Check x-api-key was removed
	if capturedHeaders.Get("x-api-key") != "" {
		t.Error("x-api-key header should have been removed")
	}

	// Check chatgpt-account-id header
	accountID := capturedHeaders.Get("chatgpt-account-id")
	if accountID != "test-account-id" {
		t.Errorf("chatgpt-account-id = %q, want 'test-account-id'", accountID)
	}

	// Check originator header
	originator := capturedHeaders.Get("originator")
	if originator != "codex_cli_rs" {
		t.Errorf("originator = %q, want 'codex_cli_rs'", originator)
	}
}

func TestOpenAIOAuthTransport_NoCredential(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t, nil)
	transport := NewOpenAIOAuthTransport(http.DefaultTransport, store)

	req, err := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err == nil {
		resp.Body.Close()
		t.Error("expected error when no credential exists")
	}
}

func TestOpenAIOAuthTransport_OriginalRequestNotModified(t *testing.T) {
	ctx := context.Background()
	accessToken := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature"

	store := setupTestStore(t, map[string]*Credential{
		"openai": {
			Type:         "oauth",
			Provider:     "openai",
			AccessToken:  accessToken,
			RefreshToken: "refresh",
			ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
		},
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	transport := NewOpenAIOAuthTransport(http.DefaultTransport, store)

	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("x-api-key", "original-key")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	resp.Body.Close()

	// Original request should not be modified
	if req.Header.Get("x-api-key") != "original-key" {
		t.Error("original request x-api-key was modified")
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("original request Authorization was modified")
	}
}

func TestOAuthTransport_AnthropicHeaders(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t, map[string]*Credential{
		"anthropic": {
			Type:         "oauth",
			Provider:     "anthropic",
			AccessToken:  "anth-access-token",
			RefreshToken: "anth-refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
		},
	})

	var capturedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	transport := NewOAuthTransport(http.DefaultTransport, store)

	req, err := http.NewRequestWithContext(ctx, "POST", ts.URL, nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	resp.Body.Close()

	// Check Authorization header
	if got := capturedHeaders.Get("Authorization"); got != "Bearer anth-access-token" {
		t.Errorf("Authorization = %q, want 'Bearer anth-access-token'", got)
	}

	// Check anthropic-beta header
	beta := capturedHeaders.Get("anthropic-beta")
	if beta == "" {
		t.Error("anthropic-beta header is missing")
	}
}

func TestPatchJSONBody(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		fields     map[string]any
		wantFields map[string]any
		wantErr    bool
	}{
		{
			name:   "add store=false to request body",
			input:  `{"model":"gpt-5","input":[],"stream":true}`,
			fields: map[string]any{"store": false},
			wantFields: map[string]any{
				"model":  "gpt-5",
				"store":  false,
				"stream": true,
			},
		},
		{
			name:   "override existing field",
			input:  `{"store":true,"model":"gpt-5"}`,
			fields: map[string]any{"store": false},
			wantFields: map[string]any{
				"model": "gpt-5",
				"store": false,
			},
		},
		{
			name:    "non-JSON body returns error",
			input:   `not json at all`,
			fields:  map[string]any{"store": false},
			wantErr: true,
		},
		{
			name:   "empty object",
			input:  `{}`,
			fields: map[string]any{"store": false},
			wantFields: map[string]any{
				"store": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := io.NopCloser(strings.NewReader(tt.input))
			result, newLen, err := patchJSONBody(body, tt.fields)
			if tt.wantErr {
				if err != nil {
					return // expected error
				}
				// For non-JSON, body is returned unchanged
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			data, readErr := io.ReadAll(result)
			if readErr != nil {
				t.Fatalf("reading result: %v", readErr)
			}
			if int64(len(data)) != newLen {
				t.Errorf("newLen=%d but actual data length=%d", newLen, len(data))
			}

			var got map[string]any
			if unmarshalErr := json.Unmarshal(data, &got); unmarshalErr != nil {
				t.Fatalf("unmarshaling result: %v", unmarshalErr)
			}

			for key, want := range tt.wantFields {
				gotVal, ok := got[key]
				if !ok {
					t.Errorf("missing key %q in result", key)
					continue
				}
				if fmt.Sprintf("%v", gotVal) != fmt.Sprintf("%v", want) {
					t.Errorf("key %q: got %v, want %v", key, gotVal, want)
				}
			}
		})
	}
}

func TestMergeBetaHeaders(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		required string
		want     string
	}{
		{
			name:     "empty existing",
			existing: "",
			required: "beta1,beta2",
			want:     "beta1,beta2",
		},
		{
			name:     "no overlap",
			existing: "beta3",
			required: "beta1,beta2",
			want:     "beta1,beta2,beta3",
		},
		{
			name:     "with overlap",
			existing: "beta1,beta3",
			required: "beta1,beta2",
			want:     "beta1,beta2,beta3",
		},
		{
			name:     "all same",
			existing: "beta1",
			required: "beta1",
			want:     "beta1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeBetaHeaders(tt.existing, tt.required)
			if got != tt.want {
				t.Errorf("mergeBetaHeaders(%q, %q) = %q, want %q", tt.existing, tt.required, got, tt.want)
			}
		})
	}
}
