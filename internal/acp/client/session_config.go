package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/spachava753/acp-sdk/acp"
)

const (
	modelRefConfigID      acp.SessionConfigId = "modelRef"
	thinkingLevelConfigID acp.SessionConfigId = "thinkingLevel"
)

func applySessionConfig(
	ctx context.Context,
	conn *acp.Client,
	sessionID acp.SessionId,
	configOptions []acp.SessionConfigOption,
	opts Options,
) ([]acp.SessionConfigOption, error) {
	if opts.ModelRef != "" {
		if err := validateConfigValue(configOptions, modelRefConfigID, opts.ModelRef); err != nil {
			return nil, err
		}
		resp, err := conn.SetSessionConfigOption(ctx, &acp.SetSessionConfigOptionRequest{
			ConfigID:  modelRefConfigID,
			SessionID: sessionID,
			Value:     acp.SessionConfigValueId(opts.ModelRef),
		})
		if err != nil {
			return nil, fmt.Errorf("could not set model %q: %w", opts.ModelRef, err)
		}
		configOptions = resp.ConfigOptions
	}

	if opts.ThinkingLevel != "" {
		if err := validateConfigValue(configOptions, thinkingLevelConfigID, opts.ThinkingLevel); err != nil {
			return nil, err
		}
		resp, err := conn.SetSessionConfigOption(ctx, &acp.SetSessionConfigOptionRequest{
			ConfigID:  thinkingLevelConfigID,
			SessionID: sessionID,
			Value:     acp.SessionConfigValueId(opts.ThinkingLevel),
		})
		if err != nil {
			return nil, fmt.Errorf("could not set thinking level %q: %w", opts.ThinkingLevel, err)
		}
		configOptions = resp.ConfigOptions
	}

	return configOptions, nil
}

func validateConfigValue(options []acp.SessionConfigOption, id acp.SessionConfigId, value string) error {
	option, ok := findConfigOption(options, id)
	if !ok {
		return fmt.Errorf("session config option %q is not available", id)
	}
	if option.Options.Ungrouped == nil {
		return fmt.Errorf("session config option %q has no selectable values", id)
	}
	for _, candidate := range *option.Options.Ungrouped {
		if string(candidate.Value) == value {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q; valid values: %s", id, value, strings.Join(configValues(option), ", "))
}

func findConfigOption(options []acp.SessionConfigOption, id acp.SessionConfigId) (acp.SessionConfigOption, bool) {
	for _, option := range options {
		if option.ID == id {
			return option, true
		}
	}
	return acp.SessionConfigOption{}, false
}

func configValues(option acp.SessionConfigOption) []string {
	if option.Options.Ungrouped == nil {
		return nil
	}
	values := make([]string, 0, len(*option.Options.Ungrouped))
	for _, candidate := range *option.Options.Ungrouped {
		values = append(values, string(candidate.Value))
	}
	return values
}
