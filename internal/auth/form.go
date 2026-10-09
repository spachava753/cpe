package auth

import (
	"context"
	"errors"
	"strings"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"

	"github.com/spachava753/cpe/internal/commandio"
)

func promptAPIKey(ctx context.Context, streams commandio.IO) (string, error) {
	input := streams.Input()
	descriptor, ok := input.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(descriptor.Fd()) {
		return "", errors.New("API-key login requires an interactive terminal")
	}
	var key string
	form := huh.NewForm(huh.NewGroup(huh.NewInput().
		Title("OpenCode Go API key").
		Description("Copy your key from https://opencode.ai/auth").
		EchoMode(huh.EchoModePassword).
		Validate(func(value string) error {
			return validateAPIKey(strings.TrimSpace(value))
		}).
		Value(&key))).
		WithInput(input).
		WithOutput(streams.ErrorOutput()).
		WithAccessible(false)
	if err := form.RunWithContext(ctx); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("API-key entry canceled")
	}
	return key, nil
}
