package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIUsageURLForBase(t *testing.T) {
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
			if got := OpenAIUsageURLForBase(tt.base); got != tt.want {
				t.Fatalf("OpenAIUsageURLForBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchOpenAIUsage(t *testing.T) {
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
				if tt.wantErrSubstr != "" && err.Error() != tt.wantErrSubstr && !contains(err.Error(), tt.wantErrSubstr) {
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
}

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && (s == substr || index(s, substr) >= 0)
}

func index(s, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
