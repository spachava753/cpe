package agent

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/spachava753/cpe/internal/config"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type validateFunc func(t *testing.T, req *http.Request)

func TestPatchTransport(t *testing.T) {
	tests := []struct {
		name        string
		patchJSON   string
		headers     map[string]string
		reqBody     string
		reqMethod   string
		expectError bool
		validate    validateFunc
	}{
		{
			name:      "headers only",
			patchJSON: "",
			headers: map[string]string{
				"X-Custom-Header": "custom-value",
				"X-Another":       "another-value",
			},
			reqBody:   "",
			reqMethod: "GET",
			validate: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("X-Custom-Header"); got != "custom-value" {
					t.Errorf("X-Custom-Header = %q, want %q", got, "custom-value")
				}
				if got := req.Header.Get("X-Another"); got != "another-value" {
					t.Errorf("X-Another = %q, want %q", got, "another-value")
				}
			},
		},
		{
			name:      "JSON patch add",
			patchJSON: `[{"op": "add", "path": "/new_field", "value": "new_value"}]`,
			reqBody:   `{"existing":"value"}`,
			reqMethod: "POST",
			validate: func(t *testing.T, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				expected := `{"existing":"value","new_field":"new_value"}`
				if string(body) != expected {
					t.Errorf("body = %q, want %q", string(body), expected)
				}
			},
		},
		{
			name:      "JSON patch replace",
			patchJSON: `[{"op": "replace", "path": "/model", "value": "custom-model"}]`,
			reqBody:   `{"model":"original-model"}`,
			reqMethod: "POST",
			validate: func(t *testing.T, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				expected := `{"model":"custom-model"}`
				if string(body) != expected {
					t.Errorf("body = %q, want %q", string(body), expected)
				}
			},
		},
		{
			name:      "JSON patch remove",
			patchJSON: `[{"op": "remove", "path": "/unwanted"}]`,
			reqBody:   `{"keep":"this","unwanted":"remove"}`,
			reqMethod: "POST",
			validate: func(t *testing.T, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				expected := `{"keep":"this"}`
				if string(body) != expected {
					t.Errorf("body = %q, want %q", string(body), expected)
				}
			},
		},
		{
			name: "multiple patches",
			patchJSON: `[
				{"op": "add", "path": "/new_field", "value": "new_value"},
				{"op": "replace", "path": "/existing", "value": "modified"}
			]`,
			reqBody:   `{"existing":"original"}`,
			reqMethod: "POST",
			validate: func(t *testing.T, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				expected := `{"existing":"modified","new_field":"new_value"}`
				if string(body) != expected {
					t.Errorf("body = %q, want %q", string(body), expected)
				}
			},
		},
		{
			name:      "no body with patches",
			patchJSON: `[{"op": "add", "path": "/new_field", "value": "new_value"}]`,
			reqBody:   "",
			reqMethod: "GET",
			validate: func(t *testing.T, req *http.Request) {
			},
		},
		{
			name:        "invalid patch",
			patchJSON:   `[{"op": "remove", "path": "/nonexistent"}]`,
			reqBody:     `{"existing":"value"}`,
			reqMethod:   "POST",
			expectError: true,
		},
		{
			name:      "headers and patches combined",
			patchJSON: `[{"op": "add", "path": "/field", "value": "data"}]`,
			headers: map[string]string{
				"X-Custom": "value",
			},
			reqBody:   `{"original":"data"}`,
			reqMethod: "POST",
			validate: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("X-Custom"); got != "value" {
					t.Errorf("X-Custom = %q, want %q", got, "value")
				}

				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				bodyStr := string(body)
				if bodyStr != `{"original":"data","field":"data"}` && bodyStr != `{"field":"data","original":"data"}` {
					t.Errorf("body = %q, want either order of field and original", bodyStr)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patches []jsonpatch.Patch
			if tt.patchJSON != "" {
				patch, err := jsonpatch.DecodePatch([]byte(tt.patchJSON))
				if err != nil {
					t.Fatalf("DecodePatch failed: %v", err)
				}
				patches = append(patches, patch)
			}

			var capturedReq *http.Request
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if tt.expectError {
					t.Error("should not reach base transport when error expected")
					return nil, nil
				}
				capturedReq = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
				}, nil
			})

			transport := NewPatchTransport(base, patches, tt.headers)

			var req *http.Request
			if tt.reqBody != "" {
				req = httptest.NewRequest(tt.reqMethod, "http://example.com", strings.NewReader(tt.reqBody))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.reqMethod, "http://example.com", nil)
			}

			_, err := transport.RoundTrip(req)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}

			if tt.validate != nil && capturedReq != nil {
				tt.validate(t, capturedReq)
			}
		})
	}
}

func TestBuildPatchTransportFromConfig(t *testing.T) {
	tests := []struct {
		name            string
		patchConfig     *config.PatchRequestConfig
		expectError     bool
		testIntegration bool
		reqBody         string
		validate        validateFunc
	}{
		{
			name:        "nil config",
			patchConfig: nil,
			expectError: false,
		},
		{
			name: "valid config with patches and headers",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]interface{}{
					{"op": "add", "path": "/field", "value": "data"},
				},
				IncludeHeaders: map[string]string{
					"X-Custom": "value",
				},
			},
			expectError: false,
		},
		{
			name: "only headers",
			patchConfig: &config.PatchRequestConfig{
				IncludeHeaders: map[string]string{
					"X-Custom": "value",
				},
			},
			expectError: false,
		},
		{
			name: "only patches",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]interface{}{
					{"op": "replace", "path": "/model", "value": "custom"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid patch operation",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]interface{}{
					{"op": "invalid", "path": "/field"},
				},
			},
			expectError: true,
		},
		{
			name: "integration test with headers and patches",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]interface{}{
					{"op": "add", "path": "/new_field", "value": "new_value"},
				},
				IncludeHeaders: map[string]string{
					"X-Test-Header": "test-value",
				},
			},
			expectError:     false,
			testIntegration: true,
			reqBody:         `{"original":"data"}`,
			validate: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("X-Test-Header"); got != "test-value" {
					t.Errorf("X-Test-Header = %q, want %q", got, "test-value")
				}

				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}

				bodyStr := string(body)
				if bodyStr != `{"original":"data","new_field":"new_value"}` && bodyStr != `{"new_field":"new_value","original":"data"}` {
					t.Errorf("unexpected body: %q", bodyStr)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base http.RoundTripper
			if tt.testIntegration {
				var capturedReq *http.Request
				base = roundTripFunc(func(req *http.Request) (*http.Response, error) {
					capturedReq = req
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
					}, nil
				})

				transport, err := BuildPatchTransportFromConfig(base, tt.patchConfig)
				if err != nil {
					t.Fatalf("BuildPatchTransportFromConfig failed: %v", err)
				}

				req := httptest.NewRequest("POST", "http://example.com", strings.NewReader(tt.reqBody))
				req.Header.Set("Content-Type", "application/json")

				_, err = transport.RoundTrip(req)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				if tt.validate != nil && capturedReq != nil {
					tt.validate(t, capturedReq)
				}
				return
			}

			transport, err := BuildPatchTransportFromConfig(base, tt.patchConfig)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.patchConfig == nil {
				if transport != nil {
					t.Error("expected nil transport when config is nil and base is nil")
				}
				return
			}

			if transport == nil {
				t.Fatal("expected non-nil transport")
			}
		})
	}
}
