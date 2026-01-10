package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// newTestStore creates a Store with credentials at a temp path for testing
func newTestStore(t *testing.T, cred *Credential) *Store {
	t.Helper()
	
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	
	creds := map[string]*Credential{
		cred.Provider: cred,
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		t.Fatalf("marshaling test credentials: %v", err)
	}
	
	if err := os.WriteFile(authPath, data, 0600); err != nil {
		t.Fatalf("writing test auth file: %v", err)
	}
	
	return &Store{path: authPath}
}

// TestOAuthTransportWithPatchedBase verifies that when OAuthTransport wraps a
// PatchTransport, both OAuth headers and JSON patches are correctly applied.
// This is the fix for GitHub issue #127.
func TestOAuthTransportWithPatchedBase(t *testing.T) {
	tests := []struct {
		name           string
		patchJSON      string
		patchHeaders   map[string]string
		requestBody    string
		wantBody       string
		wantHeaders    map[string]string
	}{
		{
			name:        "OAuth with custom headers",
			patchJSON:   "",
			patchHeaders: map[string]string{
				"X-Custom-Header": "custom-value",
			},
			requestBody: `{"model":"claude-3"}`,
			wantBody:    `{"model":"claude-3"}`,
			wantHeaders: map[string]string{
				"Authorization":    "Bearer test-access-token",
				"X-Custom-Header":  "custom-value",
			},
		},
		{
			name:        "OAuth with JSON patch to modify model",
			patchJSON:   `[{"op": "replace", "path": "/model", "value": "custom-model"}]`,
			requestBody: `{"model":"original-model","stream":true}`,
			wantBody:    `{"model":"custom-model","stream":true}`,
			wantHeaders: map[string]string{
				"Authorization": "Bearer test-access-token",
			},
		},
		{
			name:        "OAuth with JSON patch to add field",
			patchJSON:   `[{"op": "add", "path": "/custom_field", "value": "injected"}]`,
			requestBody: `{"model":"claude-3"}`,
			wantBody:    `{"custom_field":"injected","model":"claude-3"}`,
			wantHeaders: map[string]string{
				"Authorization": "Bearer test-access-token",
			},
		},
		{
			name:      "OAuth with both headers and JSON patch",
			patchJSON: `[{"op": "add", "path": "/patched", "value": true}]`,
			patchHeaders: map[string]string{
				"X-Patched-Header": "patched-value",
				"X-Another":        "another-value",
			},
			requestBody: `{"original":"data"}`,
			wantBody:    `{"original":"data","patched":true}`,
			wantHeaders: map[string]string{
				"Authorization":     "Bearer test-access-token",
				"X-Patched-Header":  "patched-value",
				"X-Another":         "another-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track what the server receives
			var receivedBody string
			receivedHeaders := make(map[string]string)

			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture headers
				for key := range tt.wantHeaders {
					receivedHeaders[key] = r.Header.Get(key)
				}

				// Capture body
				if r.Body != nil {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("reading request body: %v", err)
						http.Error(w, "error reading body", 500)
						return
					}
					receivedBody = string(body)
				}

				// Send minimal valid response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.Write([]byte(`{"type":"message","content":[]}`))
			}))
			defer server.Close()

			// Create test credential store
			store := newTestStore(t, &Credential{
				Type:         "oauth",
				Provider:     "anthropic",
				AccessToken:  "test-access-token",
				RefreshToken: "test-refresh-token",
				ExpiresAt:    9999999999, // Far future
			})

			// Build the patch transport if configured
			var patchTransport http.RoundTripper
			if tt.patchJSON != "" || len(tt.patchHeaders) > 0 {
				var patches []jsonpatch.Patch
				if tt.patchJSON != "" {
					patch, err := jsonpatch.DecodePatch([]byte(tt.patchJSON))
					if err != nil {
						t.Fatalf("decoding patch: %v", err)
					}
					patches = append(patches, patch)
				}
				patchTransport = newPatchTransport(nil, patches, tt.patchHeaders)
			}

			// Create OAuth transport with patch transport as base
			oauthTransport := NewOAuthTransport(patchTransport, store)

			// Create HTTP client with the transport chain
			client := &http.Client{Transport: oauthTransport}

			// Make request
			req, err := http.NewRequest("POST", server.URL, strings.NewReader(tt.requestBody))
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("making request: %v", err)
			}
			defer resp.Body.Close()

			// Verify headers
			for key, want := range tt.wantHeaders {
				got := receivedHeaders[key]
				if got != want {
					t.Errorf("header %q = %q, want %q", key, got, want)
				}
			}

			// Verify body (handle JSON key ordering)
			if tt.wantBody != "" {
				var gotJSON, wantJSON map[string]any
				if err := json.Unmarshal([]byte(receivedBody), &gotJSON); err != nil {
					t.Fatalf("parsing received body: %v (body was: %q)", err, receivedBody)
				}
				if err := json.Unmarshal([]byte(tt.wantBody), &wantJSON); err != nil {
					t.Fatalf("parsing expected body: %v", err)
				}

				gotBytes, _ := json.Marshal(gotJSON)
				wantBytes, _ := json.Marshal(wantJSON)
				if string(gotBytes) != string(wantBytes) {
					t.Errorf("body = %s, want %s", gotBytes, wantBytes)
				}
			}
		})
	}
}

// patchTransport is a minimal implementation for testing the transport chain
type patchTransport struct {
	base        http.RoundTripper
	jsonPatches []jsonpatch.Patch
	headers     map[string]string
}

func newPatchTransport(base http.RoundTripper, patches []jsonpatch.Patch, headers map[string]string) *patchTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &patchTransport{
		base:        base,
		jsonPatches: patches,
		headers:     headers,
	}
}

func (t *patchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Apply headers
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}

	// Apply JSON patches to body
	if req.Body != nil && len(t.jsonPatches) > 0 {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()

		for _, patch := range t.jsonPatches {
			modified, err := patch.Apply(body)
			if err != nil {
				return nil, err
			}
			body = modified
		}

		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	}

	return t.base.RoundTrip(req)
}

// TestOAuthTransportPreservesExistingBetaHeaders verifies that OAuth transport
// merges beta headers rather than overwriting them
func TestOAuthTransportPreservesExistingBetaHeaders(t *testing.T) {
	var receivedBeta string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBeta = r.Header.Get("anthropic-beta")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	store := newTestStore(t, &Credential{
		Type:         "oauth",
		Provider:     "anthropic",
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    9999999999,
	})

	transport := NewOAuthTransport(nil, store)
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("anthropic-beta", "custom-beta-feature")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Should contain both OAuth required betas and custom beta
	if !strings.Contains(receivedBeta, "oauth-2025-04-20") {
		t.Errorf("missing OAuth beta header, got: %s", receivedBeta)
	}
	if !strings.Contains(receivedBeta, "custom-beta-feature") {
		t.Errorf("missing custom beta header, got: %s", receivedBeta)
	}
}
