package codemode

const (
	charsPerToken              = 4
	contextWindowSpillFraction = 5 // 20% of context window
)

// ResolveLargeOutputCharLimit resolves the spill threshold in characters.
// If configuredLimit is set (>0), it is used as-is.
// Otherwise, the default is derived from context window tokens:
// 20% of context window * 4 chars/token.
func ResolveLargeOutputCharLimit(configuredLimit int, contextWindow uint32) int {
	if configuredLimit > 0 {
		return configuredLimit
	}
	if contextWindow == 0 {
		return 0
	}
	return int((uint64(contextWindow) * charsPerToken) / contextWindowSpillFraction)
}
