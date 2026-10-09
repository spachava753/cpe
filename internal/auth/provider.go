package auth

import (
	"errors"
	"strings"
)

// Provider identifies a subscription authentication service.
type Provider uint8

const (
	// OpenAI signs in to ChatGPT for Codex subscription access.
	OpenAI Provider = iota + 1
	// OpenCode stores an OpenCode Go API key.
	OpenCode
)

// ParseProvider accepts openai or opencode, ignoring case.
func ParseProvider(name string) (Provider, error) {
	switch strings.ToLower(name) {
	case "openai":
		return OpenAI, nil
	case "opencode":
		return OpenCode, nil
	default:
		return 0, errors.New("authentication provider must be openai or opencode")
	}
}

func (provider Provider) name() (string, error) {
	switch provider {
	case OpenAI:
		return "openai", nil
	case OpenCode:
		return "opencode", nil
	default:
		return "", errors.New("invalid authentication provider")
	}
}
