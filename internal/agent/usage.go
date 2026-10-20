package agent

import (
	"github.com/spachava753/acp-sdk/acp"

	"github.com/spachava753/cpe/internal/conversation"
)

func protocolUsage(u conversation.TokenUsage) *acp.Usage {
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

func (s *session) recordUsage(usage conversation.TokenUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.record.Usage.Add(usage)
	if err != nil {
		return err
	}
	s.record.Usage = next
	return nil
}
