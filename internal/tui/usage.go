package tui

import "fmt"

func shortTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprint(n)
	}
}

func (m model) usageLines() (string, string) {
	u := m.usage
	counts := fmt.Sprintf("In %s · Out %s · Cache read %s · write %s", shortTokens(u.Input), shortTokens(u.Output), shortTokens(u.CacheRead), shortTokens(u.CacheWrite))
	if m.width < 60 {
		counts = fmt.Sprintf("I %s O %s R %s W %s", shortTokens(u.Input), shortTokens(u.Output), shortTokens(u.CacheRead), shortTokens(u.CacheWrite))
	}
	partial := ""
	if u.Unreported > 0 || u.Legacy {
		partial = "+"
	}
	cost := fmt.Sprintf("~$%.4f", u.Cost)
	if u.Unpriced > 0 || u.Legacy {
		cost += "+?"
	}
	totals := fmt.Sprintf("Total %s%s · %s · Context ~%s", shortTokens(u.Total()), partial, cost, shortTokens(int64(m.contextTokens)))
	if m.profile.ContextWindow > 0 {
		totals += "/" + shortTokens(int64(m.profile.ContextWindow))
	}
	return counts, totals
}

func (m model) usageDetails() string {
	u := m.usage
	text := fmt.Sprintf("Session usage\n\nInput (uncached): %d\nOutput: %d\nCache read: %d\nCache write: %d\nTotal tokens: %d\nRequests: %d\nEstimated cost: $%.6f USD\n\nCurrent context: ~%d input tokens", u.Input, u.Output, u.CacheRead, u.CacheWrite, u.Total(), u.Requests, u.Cost, m.contextTokens)
	if m.profile.ContextWindow > 0 {
		text += fmt.Sprintf(" / %d budget\nAuto-compaction at ~%d tokens", m.profile.ContextWindow, m.profile.ContextWindow-m.profile.ContextWindow/10)
	} else {
		text += " (no token budget configured)"
	}
	text += "\n\nTotals include compactions and all branches in this session. Cached input is counted once per request. Output includes reported reasoning tokens."
	if u.Unreported > 0 {
		text += fmt.Sprintf("\n%d request(s) lack complete usage; totals contain only reported tokens.", u.Unreported)
	}
	if u.Unpriced > 0 {
		text += fmt.Sprintf("\n%d request(s) lack complete pricing or usage; cost is a partial estimate.", u.Unpriced)
	}
	if u.Legacy {
		text += "\nOlder history has no saved usage. Its consumption cannot be recovered."
	}
	text += "\nCosts use saved configured rates, not account billing. Context is a text estimate, not the provider's exact tokenizer count."
	return text
}
