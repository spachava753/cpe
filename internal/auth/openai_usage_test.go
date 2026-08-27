package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type usageRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f usageRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUsageURLForOpenAIBase(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "default",
			want: "https://chatgpt.com/backend-api/wham/usage",
		},
		{
			name: "backend base",
			base: "https://proxy.example.com/backend-api",
			want: "https://proxy.example.com/backend-api/wham/usage",
		},
		{
			name: "usage endpoint",
			base: "https://proxy.example.com/backend-api/wham/usage",
			want: "https://proxy.example.com/backend-api/wham/usage",
		},
		{
			name: "trailing slash",
			base: "https://proxy.example.com/backend-api/",
			want: "https://proxy.example.com/backend-api/wham/usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURL string
			client := &http.Client{Transport: usageRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				gotURL = req.URL.String()
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			})}
			if _, err := FetchOpenAIUsage(t.Context(), client, tt.base, "test-token"); err != nil {
				t.Fatalf("FetchOpenAIUsage() error = %v", err)
			}
			if gotURL != tt.want {
				t.Fatalf("request URL = %q, want %q", gotURL, tt.want)
			}
		})
	}
}

func TestFetchOpenAIUsage(t *testing.T) {
	t.Run("base case", func(t *testing.T) {
		tests := []struct {
			name           string
			accessToken    string
			statusCode     int
			responseBody   string
			wantErr        bool
			wantErrSubstr  string
			wantPlanType   string
			wantAuthHeader string
		}{
			{
				name:           "success",
				accessToken:    "test-token",
				statusCode:     http.StatusOK,
				responseBody:   `{"user_id":"user_123","account_id":"acct_123","email":"user@example.com","plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":19,"limit_window_seconds":18000,"reset_after_seconds":1200,"reset_at":1773426746},"secondary_window":{"used_percent":25,"limit_window_seconds":604800,"reset_after_seconds":426894,"reset_at":1773852365}},"code_review_rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"limit_window_seconds":604800,"reset_after_seconds":604800,"reset_at":1774030271}},"additional_rate_limits":[],"credits":{"balance":"0","has_credits":false,"unlimited":false,"approx_cloud_messages":[0,0],"approx_local_messages":[0,0]},"promo":null}`,
				wantPlanType:   "pro",
				wantAuthHeader: "Bearer test-token",
			},
			{
				name:          "missing token",
				wantErr:       true,
				wantErrSubstr: "access token is required",
			},
			{
				name:          "non-200 response",
				accessToken:   "test-token",
				statusCode:    http.StatusUnauthorized,
				responseBody:  `{"error":"unauthorized"}`,
				wantErr:       true,
				wantErrSubstr: "usage request failed (status 401)",
			},
			{
				name:          "invalid JSON success response",
				accessToken:   "test-token",
				statusCode:    http.StatusOK,
				responseBody:  `not-json`,
				wantErr:       true,
				wantErrSubstr: "parsing usage response",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var gotAuthHeader string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotAuthHeader = r.Header.Get("Authorization")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.statusCode)
					fmt.Fprint(w, tt.responseBody)
				}))
				defer server.Close()

				usage, err := FetchOpenAIUsage(context.Background(), server.Client(), server.URL, tt.accessToken)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if tt.wantErrSubstr != "" && err.Error() != tt.wantErrSubstr && !strings.Contains(err.Error(), tt.wantErrSubstr) {
						t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErrSubstr)
					}
					return
				}

				if err != nil {
					t.Fatalf("FetchOpenAIUsage() error = %v", err)
				}
				if gotAuthHeader != tt.wantAuthHeader {
					t.Fatalf("Authorization header = %q, want %q", gotAuthHeader, tt.wantAuthHeader)
				}
				if usage.PlanType != tt.wantPlanType {
					t.Fatalf("PlanType = %q, want %q", usage.PlanType, tt.wantPlanType)
				}
				if usage.RateLimit.PrimaryWindow.UsedPercent != 19 {
					t.Fatalf("PrimaryWindow.UsedPercent = %d, want 19", usage.RateLimit.PrimaryWindow.UsedPercent)
				}
			})
		}
	})
	t.Run("with default client retries retryable status", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.Header().Set("Content-Type", "application/json")
			if attempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":"temporarily unavailable"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"user_id":"user_123","account_id":"acct_123","email":"user@example.com","plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{}},"code_review_rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{}},"credits":{"balance":"0","has_credits":false,"unlimited":false},"promo":null}`)
		}))
		defer server.Close()

		usage, err := FetchOpenAIUsage(context.Background(), nil, server.URL, "test-token")
		if err != nil {
			t.Fatalf("FetchOpenAIUsage() error = %v", err)
		}
		if attempts != 2 {
			t.Fatalf("attempts = %d, want 2", attempts)
		}
		if usage.PlanType != "pro" {
			t.Fatalf("PlanType = %q, want pro", usage.PlanType)
		}
	})
}
