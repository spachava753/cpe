package opencodego

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spachava753/gai"
)

func TestServiceError(t *testing.T) {
	delay := time.Second
	for _, test := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"cancellation", context.Canceled},
		{"local failure", errors.New("local failure")},
		{"authentication", &gai.ApiErr{Provider: gai.ProviderOpenAI, Kind: gai.APIErrorKindAuthentication, StatusCode: 401, Message: "Invalid API key: fixture-secret", RawBody: "fixture-body"}},
		{"wrapped authentication", fmt.Errorf("wrapped: %w", &gai.ApiErr{Provider: gai.ProviderAnthropic, Kind: gai.APIErrorKindAuthentication, StatusCode: 401})},
		{"permission", &gai.ApiErr{Provider: gai.ProviderResponses, Kind: gai.APIErrorKindPermission, StatusCode: 403, Message: "subscription required"}},
		{"quota", &gai.ApiErr{Provider: gai.ProviderOpenAI, Kind: gai.APIErrorKindRateLimit, StatusCode: 429, Message: "quota exhausted", RetryAfterDuration: &delay}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var original *gai.ApiErr
			if !errors.As(test.err, &original) {
				got := serviceError(test.err)
				if got != test.err {
					t.Fatalf("non-API error changed: %v", got)
				}
				return
			}
			before := *original
			got := serviceError(test.err)
			var api *gai.ApiErr
			if !errors.As(got, &api) || api.Provider != credentialName || !errors.Is(got, test.err) || api == original {
				t.Fatalf("lost service identity or original error: %v", got)
			}
			if api.Kind != before.Kind || api.StatusCode != before.StatusCode || api.RawBody != before.RawBody || api.RetryAfterDuration != before.RetryAfterDuration || api.Retryable() != original.Retryable() {
				t.Fatalf("lost API details: %+v", api)
			}
			if before.Kind == gai.APIErrorKindAuthentication {
				if !strings.Contains(api.Message, "/login opencode-go") || !strings.Contains(api.Message, "https://opencode.ai/auth") || strings.Contains(api.Message, "fixture-secret") {
					t.Fatalf("missing recovery or exposed upstream credential: %v", api)
				}
			} else if api.Message != before.Message {
				t.Fatalf("non-authentication message changed: %v", api)
			}
			if *original != before {
				t.Fatal("upstream error mutated")
			}
		})
	}
}
