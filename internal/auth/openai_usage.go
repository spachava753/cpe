package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	// OpenAIUsageURL is the ChatGPT backend endpoint that returns subscription
	// usage and rate-limit information for the authenticated account.
	OpenAIUsageURL = "https://chatgpt.com/backend-api/wham/usage"
)

// OpenAIUsageWindow represents a rate-limit window in the ChatGPT usage API.
type OpenAIUsageWindow struct {
	UsedPercent        int   `json:"used_percent,omitempty"`
	LimitWindowSeconds int   `json:"limit_window_seconds,omitempty"`
	ResetAfterSeconds  int   `json:"reset_after_seconds,omitempty"`
	ResetAt            int64 `json:"reset_at,omitempty"`
}

// OpenAIRateLimit represents a rate-limit status block in the ChatGPT usage API.
type OpenAIRateLimit struct {
	Allowed         bool               `json:"allowed"`
	LimitReached    bool               `json:"limit_reached"`
	PrimaryWindow   OpenAIUsageWindow  `json:"primary_window"`
	SecondaryWindow *OpenAIUsageWindow `json:"secondary_window,omitempty"`
}

// OpenAICredits represents credit information in the ChatGPT usage API.
type OpenAICredits struct {
	Balance             string `json:"balance"`
	HasCredits          bool   `json:"has_credits"`
	Unlimited           bool   `json:"unlimited"`
	ApproxCloudMessages []int  `json:"approx_cloud_messages,omitempty"`
	ApproxLocalMessages []int  `json:"approx_local_messages,omitempty"`
}

// OpenAIAdditionalRateLimit represents an additional metered feature usage block.
type OpenAIAdditionalRateLimit struct {
	LimitName      string          `json:"limit_name"`
	MeteredFeature string          `json:"metered_feature"`
	RateLimit      OpenAIRateLimit `json:"rate_limit"`
}

// OpenAIUsageResponse is the JSON payload returned by the ChatGPT usage API.
type OpenAIUsageResponse struct {
	UserID               string                      `json:"user_id"`
	AccountID            string                      `json:"account_id"`
	Email                string                      `json:"email"`
	PlanType             string                      `json:"plan_type"`
	RateLimit            OpenAIRateLimit             `json:"rate_limit"`
	CodeReviewRateLimit  OpenAIRateLimit             `json:"code_review_rate_limit"`
	AdditionalRateLimits []OpenAIAdditionalRateLimit `json:"additional_rate_limits,omitempty"`
	Credits              OpenAICredits               `json:"credits"`
	Promo                any                         `json:"promo"`
}

// FetchOpenAIUsage retrieves subscription usage information from the ChatGPT
// backend usage endpoint using an OAuth bearer token.
func FetchOpenAIUsage(ctx context.Context, client *http.Client, usageURL, accessToken string) (*OpenAIUsageResponse, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	if usageURL == "" {
		usageURL = OpenAIUsageURL
	}
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting usage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
			return nil, fmt.Errorf("usage request failed (status %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("usage request failed (status %d): %v", resp.StatusCode, errBody)
	}

	var usage OpenAIUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("parsing usage response: %w", err)
	}

	return &usage, nil
}
