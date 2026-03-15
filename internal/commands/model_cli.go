package commands

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spachava753/cpe/internal/config"
)

// ModelListFromConfigOptions contains CLI-facing inputs for model listing.
type ModelListFromConfigOptions struct {
	ConfigPath   string
	DefaultModel string
	Writer       io.Writer
}

// ModelListFromConfig loads raw config and prints configured models.
func ModelListFromConfig(ctx context.Context, opts ModelListFromConfigOptions) error {
	cfg, err := config.LoadRawConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	defaultModel := opts.DefaultModel
	if defaultModel == "" {
		defaultModel = cfg.Defaults.Model
	}

	return ModelList(ctx, ModelListOptions{
		Config:       cfg,
		DefaultModel: defaultModel,
		Writer:       writer,
	})
}

// ModelInfoFromConfigOptions contains CLI-facing inputs for model inspection.
type ModelInfoFromConfigOptions struct {
	ConfigPath string
	ModelName  string
	Writer     io.Writer
}

// ModelInfoFromConfig loads raw config and prints one model's details.
func ModelInfoFromConfig(ctx context.Context, opts ModelInfoFromConfigOptions) error {
	cfg, err := config.LoadRawConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	return ModelInfo(ctx, ModelInfoOptions{
		Config:    cfg,
		ModelName: opts.ModelName,
		Writer:    writer,
	})
}

// ModelSystemPromptFromConfigOptions contains CLI-facing inputs for rendering a
// model system prompt.
type ModelSystemPromptFromConfigOptions struct {
	ConfigPath   string
	ModelName    string
	DefaultModel string
	Output       io.Writer
	Stderr       io.Writer
}

// ModelSystemPromptFromConfig loads raw config and renders the selected model's
// effective system prompt.
func ModelSystemPromptFromConfig(ctx context.Context, opts ModelSystemPromptFromConfigOptions) error {
	cfg, resolvedConfigPath, err := config.LoadRawConfigWithPath(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	modelName := opts.ModelName
	if modelName == "" {
		if cfg.Defaults.Model != "" {
			modelName = cfg.Defaults.Model
		} else {
			modelName = opts.DefaultModel
		}
	}

	effectiveConfig, err := config.ResolveConfig(opts.ConfigPath, config.RuntimeOptions{ModelRef: modelName})
	if err != nil {
		return fmt.Errorf("failed to resolve configuration: %w", err)
	}

	output := opts.Output
	if output == nil {
		output = os.Stdout
	}

	return ModelSystemPrompt(ctx, ModelSystemPromptOptions{
		Config:          cfg,
		EffectiveConfig: effectiveConfig,
		ConfigFilePath:  resolvedConfigPath,
		ModelName:       modelName,
		DefaultModel:    opts.DefaultModel,
		Output:          output,
		Stderr:          opts.Stderr,
	})
}
