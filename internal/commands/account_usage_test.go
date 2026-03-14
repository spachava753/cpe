package commands

import (
	"testing"
	"time"

	"github.com/spachava753/cpe/internal/auth"
)

func TestRenderResetText(t *testing.T) {
	tests := []struct {
		name   string
		window *auth.OpenAIUsageWindow
		now    time.Time
		want   string
	}{
		{
			name: "nil window",
			want: "reset unknown",
		},
		{
			name:   "prefer reset after seconds",
			window: &auth.OpenAIUsageWindow{ResetAfterSeconds: 125},
			want:   "resets in 2m 5s",
		},
		{
			name:   "calculate from reset at",
			window: &auth.OpenAIUsageWindow{ResetAt: time.Date(2026, time.March, 13, 14, 25, 0, 0, time.UTC).Unix()},
			now:    time.Date(2026, time.March, 13, 14, 20, 0, 0, time.UTC),
			want:   "resets in 5m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderResetText(tt.window, tt.now)
			if got != tt.want {
				t.Fatalf("renderResetText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHumanizeDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "seconds", d: 45 * time.Second, want: "45s"},
		{name: "minutes and seconds", d: 125 * time.Second, want: "2m 5s"},
		{name: "hours and minutes", d: 3*time.Hour + 2*time.Minute, want: "3h 2m"},
		{name: "days and hours", d: 48*time.Hour + 5*time.Hour, want: "2d 5h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanizeDuration(tt.d)
			if got != tt.want {
				t.Fatalf("humanizeDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}
