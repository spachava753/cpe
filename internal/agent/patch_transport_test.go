package agent

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	jsonpatch "github.com/evanphx/json-patch/v5"

	"github.com/spachava753/cpe/internal/config"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPatchTransport(t *testing.T) {
	tests := []struct {
		name           string
		patchJSON      string
		headers        map[string]string
		reqBody        string
		reqMethod      string
		expectError    bool
		captureHeaders []string // headers to capture for snapshot
		captureBody    bool     // whether to capture body for snapshot
	}{
		{
			name:      "headers only",
			patchJSON: "",
			headers: map[string]string{
				"X-Custom-Header": "custom-value",
				"X-Another":       "another-value",
			},
			reqBody:        "",
			reqMethod:      "GET",
			captureHeaders: []string{"X-Another", "X-Custom-Header"},
		},
		{
			name:        "JSON patch add",
			patchJSON:   `[{"op": "add", "path": "/new_field", "value": "new_value"}]`,
			reqBody:     `{"existing":"value"}`,
			reqMethod:   "POST",
			captureBody: true,
		},
		{
			name:        "JSON patch replace",
			patchJSON:   `[{"op": "replace", "path": "/model", "value": "custom-model"}]`,
			reqBody:     `{"model":"original-model"}`,
			reqMethod:   "POST",
			captureBody: true,
		},
		{
			name:        "JSON patch remove",
			patchJSON:   `[{"op": "remove", "path": "/unwanted"}]`,
			reqBody:     `{"keep":"this","unwanted":"remove"}`,
			reqMethod:   "POST",
			captureBody: true,
		},
		{
			name: "multiple patches",
			patchJSON: `[
				{"op": "add", "path": "/new_field", "value": "new_value"},
				{"op": "replace", "path": "/existing", "value": "modified"}
			]`,
			reqBody:     `{"existing":"original"}`,
			reqMethod:   "POST",
			captureBody: true,
		},
		{
			name:      "no body with patches",
			patchJSON: `[{"op": "add", "path": "/new_field", "value": "new_value"}]`,
			reqBody:   "",
			reqMethod: "GET",
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
			reqBody:        `{"original":"data"}`,
			reqMethod:      "POST",
			captureHeaders: []string{"X-Custom"},
			captureBody:    true,
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

			resp, err := transport.RoundTrip(req)
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}

			if capturedReq != nil && (len(tt.captureHeaders) > 0 || tt.captureBody) {
				snapshot := make(map[string]any)

				if len(tt.captureHeaders) > 0 {
					headers := make(map[string]string)
					for _, h := range tt.captureHeaders {
						headers[h] = capturedReq.Header.Get(h)
					}
					snapshot["headers"] = headers
				}

				if tt.captureBody {
					body, err := io.ReadAll(capturedReq.Body)
					if err != nil {
						t.Fatalf("failed to read body: %v", err)
					}
					snapshot["body"] = string(body)
				}

				cupaloy.SnapshotT(t, snapshot)
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
		captureHeaders  []string
		captureBody     bool
	}{
		{
			name:        "nil config",
			patchConfig: nil,
			expectError: false,
		},
		{
			name: "valid config with patches and headers",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]any{
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
				JSONPatch: []map[string]any{
					{"op": "replace", "path": "/model", "value": "custom"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid patch operation",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]any{
					{"op": "invalid", "path": "/field"},
				},
			},
			expectError: true,
		},
		{
			name: "integration test with headers and patches",
			patchConfig: &config.PatchRequestConfig{
				JSONPatch: []map[string]any{
					{"op": "add", "path": "/new_field", "value": "new_value"},
				},
				IncludeHeaders: map[string]string{
					"X-Test-Header": "test-value",
				},
			},
			expectError:     false,
			testIntegration: true,
			reqBody:         `{"original":"data"}`,
			captureHeaders:  []string{"X-Test-Header"},
			captureBody:     true,
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

				resp, err := transport.RoundTrip(req)
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				if capturedReq != nil && (len(tt.captureHeaders) > 0 || tt.captureBody) {
					snapshot := make(map[string]any)

					if len(tt.captureHeaders) > 0 {
						headers := make(map[string]string)
						for _, h := range tt.captureHeaders {
							headers[h] = capturedReq.Header.Get(h)
						}
						snapshot["headers"] = headers
					}

					if tt.captureBody {
						body, err := io.ReadAll(capturedReq.Body)
						if err != nil {
							t.Fatalf("failed to read body: %v", err)
						}
						snapshot["body"] = string(body)
					}

					cupaloy.SnapshotT(t, snapshot)
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
