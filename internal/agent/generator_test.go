package agent

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
			name: "empty thinking budget does nothing",
			opts: &gai.GenOpts{},
		},
		{
			name:           "sets detailed summary when thinking budget is set",
			opts:           &gai.GenOpts{ThinkingBudget: "high"},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name:           "sets detailed summary for medium budget",
			opts:           &gai.GenOpts{ThinkingBudget: "medium"},
			wantExtraArgs:  true,
			wantSummaryVal: responses.ReasoningSummaryDetailed,
		},
		{
			name:           "sets detailed summary for low budget",
			opts:           &gai.GenOpts{ThinkingBudget: "low"},
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
				ThinkingBudget: "high",
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
				ThinkingBudget: "high",
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
