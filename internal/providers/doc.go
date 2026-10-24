// Package providers constructs model adapters and manages subscription credentials.
// Anthropic adapters enable system and multi-turn prompt caching through GAI.
// A Providers instance owns credential refresh workers; callers must close it
// when its connection ends. Response retry policy belongs to the caller.
package providers
