package conversation

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/models"
)

// TokenUsage contains provider-reported counts. Nil means the category was not reported.
// Cache and reasoning counts are breakdowns of input and output, not additional tokens.
type TokenUsage struct {
	InputTokens      *uint64
	OutputTokens     *uint64
	CacheReadTokens  *uint64
	CacheWriteTokens *uint64
	ReasoningTokens  *uint64
	// Cost is in US dollars, calculated from configured model prices rather than
	// reported by the provider. Nil means no priced usage has been recorded.
	Cost *float64
}

// ParseTokenUsage reads exact, nonnegative token counts from provider metadata.
func ParseTokenUsage(metadata gai.Metadata) (TokenUsage, error) {
	var usage TokenUsage
	for _, field := range []struct {
		target **uint64
		key    string
	}{
		{
			key:    gai.UsageMetricInputTokens,
			target: &usage.InputTokens,
		},
		{
			key:    gai.UsageMetricGenerationTokens,
			target: &usage.OutputTokens,
		},
		{
			key:    gai.UsageMetricCacheReadTokens,
			target: &usage.CacheReadTokens,
		},
		{
			key:    gai.UsageMetricCacheWriteTokens,
			target: &usage.CacheWriteTokens,
		},
		{
			key:    gai.UsageMetricReasoningTokens,
			target: &usage.ReasoningTokens,
		},
	} {
		value, exists := metadata[field.key]
		if !exists {
			continue
		}
		var count int64
		var err error
		switch value := value.(type) {
		case int:
			count = int64(value)
		case json.Number:
			count, err = value.Int64()
		default:
			err = errors.New("expected an integer")
		}
		if err != nil || count < 0 {
			return TokenUsage{}, fmt.Errorf(
				"invalid provider token usage for %q: expected a nonnegative integer",
				field.key,
			)
		}
		*field.target = new(uint64(count))
	}
	return usage, nil
}

// Priced returns usage with Cost set from the model's per-million-token prices.
// Input counts include cache reads and writes, so those tokens are charged only
// at their own rates, or at the input price when a cache rate is unset. Usage
// stays unpriced when the model has no prices or the response reported neither
// input nor output.
func (u TokenUsage) Priced(model models.Model) TokenUsage {
	if model.Input == 0 && model.Output == 0 && model.CacheRead == 0 && model.CacheWrite == 0 {
		return u
	}
	if u.InputTokens == nil && u.OutputTokens == nil {
		return u
	}
	count := func(tokens *uint64) uint64 {
		if tokens == nil {
			return 0
		}
		return *tokens
	}
	read := count(u.CacheReadTokens)
	write := count(u.CacheWriteTokens)
	uncached := count(u.InputTokens)
	uncached -= min(
		uncached,
		read,
	)
	uncached -= min(
		uncached,
		write,
	)
	readPrice := cmp.Or(
		model.CacheRead,
		model.Input,
	)
	writePrice := cmp.Or(
		model.CacheWrite,
		model.Input,
	)
	cost := float64(uncached)*model.Input + float64(read)*readPrice + float64(write)*writePrice + float64(count(u.OutputTokens))*model.Output
	u.Cost = new(cost / 1_000_000)
	return u
}

// Add accumulates reported counts without mutating either operand.
func (u TokenUsage) Add(delta TokenUsage) (TokenUsage, error) {
	next := u
	for _, field := range []struct {
		target **uint64
		delta  *uint64
	}{
		{
			target: &next.InputTokens,
			delta:  delta.InputTokens,
		},
		{
			target: &next.OutputTokens,
			delta:  delta.OutputTokens,
		},
		{
			target: &next.CacheReadTokens,
			delta:  delta.CacheReadTokens,
		},
		{
			target: &next.CacheWriteTokens,
			delta:  delta.CacheWriteTokens,
		},
		{
			target: &next.ReasoningTokens,
			delta:  delta.ReasoningTokens,
		},
	} {
		if field.delta == nil {
			continue
		}
		count := *field.delta
		if previous := *field.target; previous != nil {
			if count > math.MaxUint64-*previous {
				return TokenUsage{}, errors.New("cumulative token usage overflow")
			}
			count += *previous
		}
		// Counters are immutable: saved records and prompt replies may share old values.
		*field.target = new(count)
	}
	if delta.Cost != nil {
		cost := *delta.Cost
		if u.Cost != nil {
			cost += *u.Cost
		}
		next.Cost = new(cost)
	}
	return next, next.check()
}

func (u TokenUsage) check() error {
	if u.InputTokens != nil && u.OutputTokens != nil && *u.InputTokens > math.MaxUint64-*u.OutputTokens {
		return errors.New("total token usage overflow")
	}
	if u.Cost != nil && (*u.Cost < 0 || math.IsNaN(*u.Cost) || math.IsInf(
		*u.Cost,
		0,
	)) {
		return errors.New("invalid cumulative cost")
	}
	return nil
}
