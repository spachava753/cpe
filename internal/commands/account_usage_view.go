package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spachava753/cpe/internal/auth"
)

type openAIUsageViewOptions struct {
	Now         time.Time
	LastUpdated time.Time
	Width       int
	Watch       bool
	Loading     bool
	StatusError error
}

type openAIUsageResultMsg struct {
	usage      *auth.OpenAIUsageResponse
	fetchedAt  time.Time
	fetchError error
}

type openAIUsageRefreshMsg time.Time

type openAIUsageWatchModel struct {
	ctx         context.Context
	width       int
	usage       *auth.OpenAIUsageResponse
	lastUpdated time.Time
	loading     bool
	statusErr   error
}

func newOpenAIUsageWatchModel(ctx context.Context) openAIUsageWatchModel {
	return openAIUsageWatchModel{
		ctx:     ctx,
		width:   80,
		loading: true,
	}
}

func (m openAIUsageWatchModel) Init() tea.Cmd {
	return fetchOpenAIUsageCmd(m.ctx)
}

func (m openAIUsageWatchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		return m, nil
	case openAIUsageRefreshMsg:
		m.loading = true
		return m, fetchOpenAIUsageCmd(m.ctx)
	case openAIUsageResultMsg:
		m.loading = false
		if msg.fetchError != nil {
			m.statusErr = msg.fetchError
		} else {
			m.usage = msg.usage
			m.lastUpdated = msg.fetchedAt
			m.statusErr = nil
		}
		return m, scheduleOpenAIUsageRefresh()
	}
	return m, nil
}

func (m openAIUsageWatchModel) View() string {
	return renderOpenAIUsageView(m.usage, openAIUsageViewOptions{
		Now:         time.Now(),
		LastUpdated: m.lastUpdated,
		Width:       m.width,
		Watch:       true,
		Loading:     m.loading,
		StatusError: m.statusErr,
	})
}

func fetchOpenAIUsageCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		usage, err := fetchOpenAIAccountUsage(ctx)
		return openAIUsageResultMsg{
			usage:      usage,
			fetchedAt:  time.Now(),
			fetchError: err,
		}
	}
}

func scheduleOpenAIUsageRefresh() tea.Cmd {
	return tea.Tick(accountUsageRefreshInterval, func(t time.Time) tea.Msg {
		return openAIUsageRefreshMsg(t)
	})
}

func renderOpenAIUsageView(usage *auth.OpenAIUsageResponse, opts openAIUsageViewOptions) string {
	width := maxInt(60, opts.Width)
	var lines []string

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#A78BFA"})
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"})
	labelStyle := lipgloss.NewStyle().Bold(true)
	valueStyle := lipgloss.NewStyle().Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B00020", Dark: "#FF6B6B"})

	lines = append(lines, titleStyle.Render("OpenAI account usage"))
	if usage != nil {
		if identity := joinUsageFields(strings.TrimSpace(usage.Email), strings.ToUpper(strings.TrimSpace(usage.PlanType))); identity != "" {
			lines = append(lines, identity)
		}
	}
	if !opts.LastUpdated.IsZero() {
		lines = append(lines, mutedStyle.Render("Updated "+opts.LastUpdated.UTC().Format("2006-01-02 15:04:05 MST")))
	}
	if opts.StatusError != nil {
		lines = append(lines, errorStyle.Render("Last refresh failed: "+opts.StatusError.Error()))
	}
	if usage == nil {
		if opts.Loading {
			lines = append(lines, "", "Loading usage...")
		} else {
			lines = append(lines, "", "No usage data available.")
		}
		if opts.Watch {
			lines = append(lines, "", mutedStyle.Render("Watching live • refreshes every 1s • press q to quit"))
		}
		return strings.Join(lines, "\n")
	}

	barWidth := clampInt(width-28, 18, 44)
	lines = append(lines, "")
	lines = append(lines, renderUsageWindowSection("Primary window (5h)", &usage.RateLimit.PrimaryWindow, &usage.RateLimit, opts.Now, barWidth, labelStyle, valueStyle, mutedStyle))
	lines = append(lines, "")
	lines = append(lines, renderUsageWindowSection("Secondary window (weekly)", usage.RateLimit.SecondaryWindow, &usage.RateLimit, opts.Now, barWidth, labelStyle, valueStyle, mutedStyle))

	if len(usage.AdditionalRateLimits) > 0 {
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("Additional metered limits"))
		for i, additional := range usage.AdditionalRateLimits {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, renderAdditionalRateLimitSection(additional, opts.Now, barWidth, labelStyle, valueStyle, mutedStyle))
		}
	}
	if opts.Watch {
		lines = append(lines, "")
		lines = append(lines, mutedStyle.Render("Watching live • refreshes every 1s • press q to quit"))
	}
	return strings.Join(lines, "\n")
}

func renderUsageWindowSection(title string, window *auth.OpenAIUsageWindow, limit *auth.OpenAIRateLimit, now time.Time, barWidth int, labelStyle, valueStyle, mutedStyle lipgloss.Style) string {
	var lines []string
	lines = append(lines, labelStyle.Render(title))
	if window == nil {
		lines = append(lines, "Unavailable in the current response")
		return strings.Join(lines, "\n")
	}

	meter := progress.New(
		progress.WithWidth(barWidth),
		progress.WithSolidFill("#5A56E0"),
		progress.WithFillCharacters('█', '░'),
		progress.WithoutPercentage(),
	)
	bar := meter.ViewAs(clampPercent(float64(window.UsedPercent) / 100))
	lines = append(lines, bar+"  "+valueStyle.Render(fmt.Sprintf("%d%% used", window.UsedPercent)))
	lines = append(lines, mutedStyle.Render(renderWindowStatus(window, limit, now)))
	return strings.Join(lines, "\n")
}

func renderAdditionalRateLimitSection(additional auth.OpenAIAdditionalRateLimit, now time.Time, barWidth int, labelStyle, valueStyle, mutedStyle lipgloss.Style) string {
	var lines []string

	header := joinUsageFields(strings.TrimSpace(additional.LimitName), strings.TrimSpace(additional.MeteredFeature))
	if header == "" {
		header = "Unnamed metered limit"
	}
	lines = append(lines, labelStyle.Render(header))
	lines = append(lines, renderUsageWindowSection("  Primary window (5h)", &additional.RateLimit.PrimaryWindow, &additional.RateLimit, now, barWidth, labelStyle, valueStyle, mutedStyle))
	lines = append(lines, renderUsageWindowSection("  Secondary window (weekly)", additional.RateLimit.SecondaryWindow, &additional.RateLimit, now, barWidth, labelStyle, valueStyle, mutedStyle))
	return strings.Join(lines, "\n")
}

func joinUsageFields(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " • ")
}

func renderWindowStatus(window *auth.OpenAIUsageWindow, limit *auth.OpenAIRateLimit, now time.Time) string {
	status := "Allowed now"
	if limit != nil {
		switch {
		case limit.LimitReached:
			status = "Limit reached"
		case !limit.Allowed:
			status = "Not currently allowed"
		}
	}
	return status + " • " + renderResetText(window, now)
}

func renderResetText(window *auth.OpenAIUsageWindow, now time.Time) string {
	if window == nil {
		return "reset unknown"
	}
	if window.ResetAfterSeconds > 0 {
		return "resets in " + humanizeDuration(time.Duration(window.ResetAfterSeconds)*time.Second)
	}
	if window.ResetAt > 0 {
		remaining := time.Until(time.Unix(window.ResetAt, 0))
		if !now.IsZero() {
			remaining = time.Unix(window.ResetAt, 0).Sub(now)
		}
		if remaining < 0 {
			remaining = 0
		}
		return "resets in " + humanizeDuration(remaining)
	}
	return "reset unknown"
}

func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second).Seconds())
	days := seconds / 86400
	seconds %= 86400
	hours := seconds / 3600
	seconds %= 3600
	minutes := seconds / 60
	seconds %= 60

	parts := make([]string, 0, 2)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if days == 0 && minutes > 0 && len(parts) < 2 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if days == 0 && hours == 0 && seconds > 0 && len(parts) < 2 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	if len(parts) == 0 {
		return "0s"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, " ")
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampInt(v, minValue, maxValue int) int {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
