package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/spachava753/gai"
)

// TokenUsage contains provider-reported counts. Nil means the category was not reported.
// Cache and reasoning counts are breakdowns of input and output, not additional tokens.
type TokenUsage struct {
	InputTokens      *uint64
	OutputTokens     *uint64
	CacheReadTokens  *uint64
	CacheWriteTokens *uint64
	ReasoningTokens  *uint64
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
	return next, next.check()
}

func (u TokenUsage) check() error {
	if u.InputTokens != nil && u.OutputTokens != nil && *u.InputTokens > math.MaxUint64-*u.OutputTokens {
		return errors.New("total token usage overflow")
	}
	return nil
}
