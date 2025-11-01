package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/spachava753/cpe/internal/config"
)

// ModelListOptions contains parameters for listing models
type ModelListOptions struct {
	Config       *config.Config
	DefaultModel string
	Writer       io.Writer
}

// ModelList lists all configured models
func ModelList(ctx context.Context, opts ModelListOptions) error {
	for _, model := range opts.Config.Models {
		line := model.Ref
		if opts.DefaultModel != "" && model.Ref == opts.DefaultModel {
			line += " (default)"
		}
		fmt.Fprintln(opts.Writer, line)
	}
	return nil
}

// ModelInfoOptions contains parameters for showing model details
type ModelInfoOptions struct {
	Config    *config.Config
	ModelName string
	Writer    io.Writer
}

// ModelInfo displays detailed information about a specific model
func ModelInfo(ctx context.Context, opts ModelInfoOptions) error {
	if opts.ModelName == "" {
		return fmt.Errorf("no model name provided")
	}

	model, found := opts.Config.FindModel(opts.ModelName)
	if !found {
		return fmt.Errorf("model %q not found", opts.ModelName)
	}

	fmt.Fprintf(opts.Writer, "Ref: %s\nDisplay Name: %s\nType: %s\nID: %s\nContext: %d\nMaxOutput: %d\nInputCostPerMillion: %.6f\nOutputCostPerMillion: %.6f\n",
		model.Ref, model.DisplayName, model.Type, model.ID, model.ContextWindow, model.MaxOutput, model.InputCostPerMillion, model.OutputCostPerMillion,
	)

	if model.GenerationDefaults != nil {
		fmt.Fprintln(opts.Writer, "\nGeneration Defaults:")
		if model.GenerationDefaults.Temperature != nil {
			fmt.Fprintf(opts.Writer, "  Temperature: %.2f\n", *model.GenerationDefaults.Temperature)
		}
		if model.GenerationDefaults.TopP != nil {
			fmt.Fprintf(opts.Writer, "  TopP: %.2f\n", *model.GenerationDefaults.TopP)
		}
		if model.GenerationDefaults.TopK != nil {
			fmt.Fprintf(opts.Writer, "  TopK: %d\n", *model.GenerationDefaults.TopK)
		}
		if model.GenerationDefaults.MaxTokens != nil {
			fmt.Fprintf(opts.Writer, "  MaxTokens: %d\n", *model.GenerationDefaults.MaxTokens)
		}
		if model.GenerationDefaults.ThinkingBudget != nil {
			fmt.Fprintf(opts.Writer, "  ThinkingBudget: %s\n", *model.GenerationDefaults.ThinkingBudget)
		}
	}

	return nil
}

// SystemPromptRenderer renders system prompts with template evaluation
type SystemPromptRenderer interface {
	Render(template string, model *config.Model) (string, error)
}

// ModelSystemPromptOptions contains parameters for showing system prompts
type ModelSystemPromptOptions struct {
	Config               *config.Config
	ModelName            string
	SystemPromptTemplate string
	SystemPromptPath     string // For display purposes only
	Writer               io.Writer
	SystemPromptRenderer SystemPromptRenderer
}

// ModelSystemPrompt displays the rendered system prompt for a model
func ModelSystemPrompt(ctx context.Context, opts ModelSystemPromptOptions) error {
	if opts.ModelName == "" {
		return fmt.Errorf("no model specified")
	}

	selectedModel, found := opts.Config.FindModel(opts.ModelName)
	if !found {
		return fmt.Errorf("model %q not found in configuration", opts.ModelName)
	}

	if opts.SystemPromptTemplate == "" {
		fmt.Fprintf(opts.Writer, "Model %q does not define a system prompt.\n", opts.ModelName)
		return nil
	}

	rendered, err := opts.SystemPromptRenderer.Render(opts.SystemPromptTemplate, &selectedModel.Model)
	if err != nil {
		return fmt.Errorf("failed to render system prompt: %w", err)
	}

	fmt.Fprintf(opts.Writer, "Model: %s\nPath: %s\n\n%s\n", opts.ModelName, opts.SystemPromptPath, rendered)
	return nil
}
