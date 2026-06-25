package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	a "github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/spachava753/cpe/internal/config"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

const testGoogleBearerToken = "Bearer google-token"

func useStaticGoogleCredentials(t *testing.T, token string) {
	t.Helper()
	original := findDefaultGoogleCredentials
	findDefaultGoogleCredentials = func(context.Context, ...string) (*google.Credentials, error) {
		return &google.Credentials{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
		}, nil
	}
	t.Cleanup(func() {
		findDefaultGoogleCredentials = original
	})
}

func TestHTTPClientConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		transport   http.RoundTripper
		wantTimeout time.Duration
	}{
		{
			name:        "uses provided transport and timeout",
			transport:   http.DefaultTransport,
			wantTimeout: time.Hour,
		},
		{
			name:        "leaves transport nil to use defaults",
			transport:   nil,
			wantTimeout: 90 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &http.Client{Transport: tt.transport, Timeout: tt.wantTimeout}
			if got.Timeout != tt.wantTimeout {
				t.Fatalf("client timeout = %v, want %v", got.Timeout, tt.wantTimeout)
			}
			if got.Transport != tt.transport {
				t.Fatal("client transport did not preserve configured transport")
			}
		})
	}
}

func TestModelRoundTripperRetriesRetryableResponseAndReplaysBody(t *testing.T) {
	const payload = `{"input":"hello"}`
	attempts := 0
	var bodies []string

	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, string(body))
		status := http.StatusOK
		if attempts == 1 {
			status = http.StatusInternalServerError
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(bytes.NewBufferString("{}")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	client := &http.Client{Transport: newModelRoundTripper(base)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/v1/messages", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(bodies) != 2 || bodies[0] != payload || bodies[1] != payload {
		t.Fatalf("bodies = %#v, want payload replayed twice", bodies)
	}
}

func TestInitGeneratorFromModel_WrapsOnlyResponsesWithPhaseRetry(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "test-key")

	responsesGen, err := InitGeneratorFromModel(t.Context(), config.Model{
		ID:        "gpt-5",
		Type:      ModelTypeResponses,
		ApiKeyEnv: "TEST_OPENAI_API_KEY",
	}, "", time.Minute)
	if err != nil {
		t.Fatalf("initGeneratorFromModel responses error = %v", err)
	}
	if _, ok := responsesGen.(*responsesPhaseRetryGenerator); !ok {
		t.Fatalf("responses generator type = %T, want *responsesPhaseRetryGenerator", responsesGen)
	}

	openAIGen, err := InitGeneratorFromModel(t.Context(), config.Model{
		ID:        "gpt-4o",
		Type:      "openai",
		ApiKeyEnv: "TEST_OPENAI_API_KEY",
	}, "", time.Minute)
	if err != nil {
		t.Fatalf("initGeneratorFromModel openai error = %v", err)
	}
	if _, ok := openAIGen.(*responsesPhaseRetryGenerator); ok {
		t.Fatalf("openai generator type = %T, did not want *responsesPhaseRetryGenerator", openAIGen)
	}
}

func TestInitGeneratorFromModel_AnthropicVertexRequiresVertexConfig(t *testing.T) {
	_, err := InitGeneratorFromModel(t.Context(), config.Model{
		ID:   "claude-sonnet-4-6",
		Type: modelTypeAnthropicVertex,
	}, "", time.Minute)
	if err == nil {
		t.Fatal("expected missing vertex config error")
	}
	want := "vertex configuration is required for anthropic_vertex models"
	if err.Error() != want {
		t.Fatalf("unexpected error: got %q want %q", err.Error(), want)
	}
}

func TestVertexScopes(t *testing.T) {
	if got := vertexScopes(&config.VertexConfig{}); !slices.Equal(got, []string{googleCloudPlatformScope}) {
		t.Fatalf("vertexScopes() = %#v, want cloud-platform default", got)
	}

	customScopes := []string{"https://example.test/scope-a", "https://example.test/scope-b"}
	if got := vertexScopes(&config.VertexConfig{Scopes: customScopes}); !slices.Equal(got, customScopes) {
		t.Fatalf("vertexScopes() = %#v, want %#v", got, customScopes)
	}
}

func TestAnthropicVertexRequestOptionsUseGoogleAuthAndVertexRequestShape(t *testing.T) {
	const modelID = "claude-sonnet-4-6"
	tests := []struct {
		name      string
		apiKey    string
		authToken string
	}{
		{
			name:   "ignores Anthropic API key env",
			apiKey: "anthropic-api-key",
		},
		{
			name:      "ignores Anthropic auth token env",
			authToken: "anthropic-auth-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", tt.apiKey)
			t.Setenv("ANTHROPIC_AUTH_TOKEN", tt.authToken)
			t.Setenv("ANTHROPIC_BASE_URL", "https://anthropic-env.invalid")

			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want %s", r.Method, http.MethodPost)
					http.Error(w, "unexpected method", http.StatusBadRequest)
					return
				}
				wantPath := fmt.Sprintf("/v1/projects/test-project/locations/global/publishers/anthropic/models/%s:rawPredict", modelID)
				if r.URL.Path != wantPath {
					t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
					http.Error(w, "unexpected path", http.StatusBadRequest)
					return
				}
				if got := r.Header.Get("X-Api-Key"); got != "" {
					t.Errorf("X-Api-Key header = %q, want empty", got)
					http.Error(w, "unexpected Anthropic API key", http.StatusBadRequest)
					return
				}
				if got := r.Header.Get("Authorization"); got != testGoogleBearerToken {
					t.Errorf("Authorization header = %q, want Google bearer token", got)
					http.Error(w, "unexpected Authorization", http.StatusBadRequest)
					return
				}
				if got := r.Header.Get("anthropic-beta"); got != anthropicFeatureBetaHeader {
					t.Errorf("anthropic-beta header = %q, want %q", got, anthropicFeatureBetaHeader)
					http.Error(w, "unexpected beta header", http.StatusBadRequest)
					return
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("ReadAll() error = %v", err)
					http.Error(w, "read body", http.StatusBadRequest)
					return
				}
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Errorf("request body is not JSON: %v", err)
					http.Error(w, "bad json", http.StatusBadRequest)
					return
				}
				if got := payload["anthropic_version"]; got != "vertex-2023-10-16" {
					t.Errorf("anthropic_version = %#v, want vertex-2023-10-16", got)
					http.Error(w, "unexpected anthropic_version", http.StatusBadRequest)
					return
				}
				if _, ok := payload["model"]; ok {
					t.Errorf("request body still contains model: %s", body)
					http.Error(w, "model was not removed", http.StatusBadRequest)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-sonnet-4-6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer server.Close()

			useStaticGoogleCredentials(t, "google-token")
			opts, err := anthropicVertexRequestOptions(t.Context(), &config.VertexConfig{
				ProjectID: "test-project",
				Region:    "global",
			}, nil, time.Minute)
			if err != nil {
				t.Fatalf("anthropicVertexRequestOptions() error = %v", err)
			}
			opts = append(opts, aopts.WithBaseURL(server.URL))
			client := a.NewClient(opts...)
			_, err = client.Messages.New(t.Context(), a.MessageNewParams{
				MaxTokens: 1,
				Messages:  []a.MessageParam{a.NewUserMessage(a.NewTextBlock("hello"))},
				Model:     a.Model(modelID),
			})
			if err != nil {
				t.Fatalf("Messages.New() error = %v", err)
			}
			if requestCount != 1 {
				t.Fatalf("request count = %d, want 1", requestCount)
			}
		})
	}
}

func TestAnthropicVertexRequestOptionsRetryResourceExhausted(t *testing.T) {
	const modelID = "claude-sonnet-4-6"
	requestCount := 0
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		wantPath := fmt.Sprintf("/v1/projects/test-project/locations/global/publishers/anthropic/models/%s:rawPredict", modelID)
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Authorization"); got != testGoogleBearerToken {
			t.Errorf("Authorization header = %q, want Google bearer token", got)
			http.Error(w, "unexpected Authorization", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		bodies = append(bodies, string(body))
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("request body is not JSON: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if got := payload["anthropic_version"]; got != "vertex-2023-10-16" {
			t.Errorf("anthropic_version = %#v, want vertex-2023-10-16", got)
			http.Error(w, "unexpected anthropic_version", http.StatusBadRequest)
			return
		}
		if _, ok := payload["model"]; ok {
			t.Errorf("request body still contains model: %s", body)
			http.Error(w, "model was not removed", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"quota exhausted","type":"RESOURCE_EXHAUSTED"}}`)
			return
		}
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-sonnet-4-6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	useStaticGoogleCredentials(t, "google-token")
	opts, err := anthropicVertexRequestOptions(t.Context(), &config.VertexConfig{
		ProjectID: "test-project",
		Region:    "global",
	}, nil, time.Minute)
	if err != nil {
		t.Fatalf("anthropicVertexRequestOptions() error = %v", err)
	}
	opts = append(opts, aopts.WithBaseURL(server.URL))
	client := a.NewClient(opts...)
	_, err = client.Messages.New(t.Context(), a.MessageNewParams{
		MaxTokens: 1,
		Messages:  []a.MessageParam{a.NewUserMessage(a.NewTextBlock("hello"))},
		Model:     a.Model(modelID),
	})
	if err != nil {
		t.Fatalf("Messages.New() error = %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("bodies = %#v, want identical replayed Vertex requests", bodies)
	}
}

func TestAnthropicVertexRequestOptionsApplyPatchRequest(t *testing.T) {
	const modelID = "claude-sonnet-4-6"
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		wantPath := fmt.Sprintf("/v1/projects/test-project/locations/global/publishers/anthropic/models/%s:rawPredict", modelID)
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Authorization"); got != testGoogleBearerToken {
			t.Errorf("Authorization header = %q, want Google bearer token", got)
			http.Error(w, "unexpected Authorization", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("X-Vertex-Test"); got != "patched" {
			t.Errorf("X-Vertex-Test header = %q, want patched", got)
			http.Error(w, "missing patched header", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("request body is not JSON: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if got := payload["anthropic_version"]; got != "patched-vertex-version" {
			t.Errorf("anthropic_version = %#v, want patched-vertex-version", got)
			http.Error(w, "unexpected anthropic_version", http.StatusBadRequest)
			return
		}
		metadata, ok := payload["metadata"].(map[string]any)
		if !ok || metadata["user_id"] != "patched-user" {
			t.Errorf("metadata = %#v, want patched user_id", payload["metadata"])
			http.Error(w, "unexpected metadata", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"quota exhausted","type":"RESOURCE_EXHAUSTED"}}`)
			return
		}
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-sonnet-4-6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	useStaticGoogleCredentials(t, "google-token")
	patchConfig := &config.PatchRequestConfig{
		JSONPatch: []map[string]any{
			{"op": "replace", "path": "/anthropic_version", "value": "patched-vertex-version"},
			{"op": "add", "path": "/metadata", "value": map[string]any{"user_id": "patched-user"}},
		},
		IncludeHeaders: map[string]string{"X-Vertex-Test": "patched"},
	}
	opts, err := anthropicVertexRequestOptions(t.Context(), &config.VertexConfig{
		ProjectID: "test-project",
		Region:    "global",
	}, patchConfig, time.Minute)
	if err != nil {
		t.Fatalf("anthropicVertexRequestOptions() error = %v", err)
	}
	opts = append(opts, aopts.WithBaseURL(server.URL))
	client := a.NewClient(opts...)
	_, err = client.Messages.New(t.Context(), a.MessageNewParams{
		MaxTokens: 1,
		Messages:  []a.MessageParam{a.NewUserMessage(a.NewTextBlock("hello"))},
		Model:     a.Model(modelID),
	})
	if err != nil {
		t.Fatalf("Messages.New() error = %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

func TestApplyResponsesThinkingSummary(t *testing.T) {
	tests := []struct {
		name           string
		opts           *gai.GenOpts
		wantExtraArgs  bool
		wantSummaryVal any
	}{
		{
			name: "nil opts is safe",
			opts: nil,
		},
		{
			name:           "sets detailed summary without thinking budget",
			opts:           &gai.GenOpts{},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name:           "keeps thinking budget while setting detailed summary",
			opts:           &gai.GenOpts{ThinkingBudget: "high"},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name: "creates ExtraArgs map when nil",
			opts: &gai.GenOpts{
				ThinkingBudget: "high",
				ExtraArgs:      nil,
			},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name: "does not override existing summary param",
			opts: &gai.GenOpts{
				ExtraArgs: map[string]any{
					gai.ResponsesThoughtSummaryDetailParam: responses.ReasoningSummaryConcise,
				},
			},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryConcise,
		},
		{
			name: "preserves other ExtraArgs keys",
			opts: &gai.GenOpts{
				ExtraArgs: map[string]any{
					"other_key": "other_value",
				},
			},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture original ExtraArgs keys for preservation check
			var originalKeys map[string]any
			if tt.opts != nil && tt.opts.ExtraArgs != nil {
				originalKeys = make(map[string]any, len(tt.opts.ExtraArgs))
				maps.Copy(originalKeys, tt.opts.ExtraArgs)
			}

			ApplyResponsesThinkingSummary(tt.opts)

			if tt.opts == nil {
				return // nothing to check for nil opts
			}

			if tt.wantExtraArgs {
				if tt.opts.ExtraArgs == nil {
					t.Fatal("expected ExtraArgs to be non-nil")
				}
				val, exists := tt.opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]
				if !exists {
					t.Fatalf("expected %s in ExtraArgs", gai.ResponsesThoughtSummaryDetailParam)
				}
				if val != tt.wantSummaryVal {
					t.Errorf("expected summary param = %v, got %v", tt.wantSummaryVal, val)
				}

				// Verify other keys are preserved
				for k, v := range originalKeys {
					if k == gai.ResponsesThoughtSummaryDetailParam {
						continue
					}
					if tt.opts.ExtraArgs[k] != v {
						t.Errorf("ExtraArgs key %q was modified: want %v, got %v", k, v, tt.opts.ExtraArgs[k])
					}
				}
			} else {
				if tt.opts.ExtraArgs != nil {
					if _, exists := tt.opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]; exists {
						t.Errorf("did not expect %s in ExtraArgs", gai.ResponsesThoughtSummaryDetailParam)
					}
				}
			}
		})
	}
}
