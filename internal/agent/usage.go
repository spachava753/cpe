package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/spachava753/acp-sdk/acp"
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

func parseTokenUsage(metadata gai.Metadata) (TokenUsage, error) {
	var usage TokenUsage
	for _, field := range []struct {
		key    string
		target **uint64
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

func (u TokenUsage) add(delta TokenUsage) (TokenUsage, error) {
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

func (u TokenUsage) protocolUsage() *acp.Usage {
	if u.InputTokens == nil || u.OutputTokens == nil {
		return nil
	}
	return &acp.Usage{
		InputTokens:       *u.InputTokens,
		OutputTokens:      *u.OutputTokens,
		TotalTokens:       *u.InputTokens + *u.OutputTokens,
		CachedReadTokens:  u.CacheReadTokens,
		CachedWriteTokens: u.CacheWriteTokens,
		ThoughtTokens:     u.ReasoningTokens,
	}
}

func (s *session) recordUsage(usage TokenUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.record.Usage.add(usage)
	if err != nil {
		return err
	}
	s.record.Usage = next
	return nil
}
