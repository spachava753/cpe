package account

import (
	"regexp"
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/auth"
)

func TestAccountUsageRejectsRawAndWatch(t *testing.T) {
	err := AccountUsage(t.Context(), AccountUsageOptions{
		Provider: providerOpenAI,
		Raw:      true,
		Watch:    true,
	})
	if err == nil {
		t.Fatal("AccountUsage() error = nil, want raw/watch conflict")
	}
	if err.Error() != "--raw and --watch cannot be used together" {
		t.Fatalf("AccountUsage() error = %q, want raw/watch conflict", err.Error())
	}
}

func TestOpenAIUsageViewRendering(t *testing.T) {
	now := time.Date(2026, time.March, 13, 14, 20, 0, 0, time.UTC)
	view := renderOpenAIUsageView(&auth.OpenAIUsageResponse{
		Email:    "user@example.com",
		PlanType: "pro",
		RateLimit: auth.OpenAIRateLimit{
			Allowed:      true,
			LimitReached: false,
			PrimaryWindow: auth.OpenAIUsageWindow{
				UsedPercent:       19,
				ResetAfterSeconds: 1276,
			},
			SecondaryWindow: &auth.OpenAIUsageWindow{
				UsedPercent:       25,
				ResetAfterSeconds: 426894,
			},
		},
		AdditionalRateLimits: []auth.OpenAIAdditionalRateLimit{{
			LimitName: "extra",
			RateLimit: auth.OpenAIRateLimit{Allowed: true},
		}},
	}, openAIUsageViewOptions{
		Now:         now,
		LastUpdated: now,
		Width:       80,
		Watch:       true,
	})

	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	got := re.ReplaceAllString(view, "")
	want := "OpenAI account usage\nuser@example.com • PRO\nUpdated 2026-03-13 14:20:00 UTC\n\nPrimary window (5h)\n████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  19% used\nAllowed now • resets in 21m 16s\n\nSecondary window (weekly)\n███████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  25% used\nAllowed now • resets in 4d 22h\n\nAdditional metered limits\nextra\n  Primary window (5h)\n░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  0% used\nAllowed now • reset unknown\n  Secondary window (weekly)\nUnavailable in the current response\n\nWatching live • refreshes every 1s • press q to quit"
	if got != want {
		t.Fatalf("renderOpenAIUsageView() = %q, want %q", got, want)
	}
}

func TestJoinUsageFields(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "both values present",
			values: []string{"user@example.com", "PRO"},
			want:   "user@example.com • PRO",
		},
		{
			name:   "skip empty left value",
			values: []string{"", "PRO"},
			want:   "PRO",
		},
		{
			name:   "skip empty right value",
			values: []string{"user@example.com", ""},
			want:   "user@example.com",
		},
		{
			name:   "skip whitespace-only values",
			values: []string{"  ", "\t"},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinUsageFields(tt.values...)
			if got != tt.want {
				t.Fatalf("joinUsageFields() = %q, want %q", got, tt.want)
			}
		})
	}
}
