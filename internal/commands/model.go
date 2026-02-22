package commands

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spachava753/cpe/internal/agent"
	"github.com/spachava753/cpe/internal/config"
)

// ModelListOptions contains parameters for listing models
type ModelListOptions struct {
	Config       *config.RawConfig
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
	Config    *config.RawConfig
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

	fmt.Fprintf(opts.Writer, "Ref: %s\nDisplay Name: %s\nType: %s\nID: %s\nContext: %d\nMaxOutput: %d\nInputCostPerMillion: %s\nOutputCostPerMillion: %s\nCacheReadCostPerMillion: %s\nCacheWriteCostPerMillion: %s\n",
		model.Ref,
		model.DisplayName,
		model.Type,
		model.ID,
		model.ContextWindow,
		model.MaxOutput,
		formatCostPerMillion(model.InputCostPerMillion),
		formatCostPerMillion(model.OutputCostPerMillion),
		formatCostPerMillion(model.CacheReadCostPerMillion),
		formatCostPerMillion(model.CacheWriteCostPerMillion),
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
		if model.GenerationDefaults.MaxGenerationTokens != nil {
			fmt.Fprintf(opts.Writer, "  MaxTokens: %d\n", *model.GenerationDefaults.MaxGenerationTokens)
		}
		if model.GenerationDefaults.ThinkingBudget != "" {
			fmt.Fprintf(opts.Writer, "  ThinkingBudget: %s\n", model.GenerationDefaults.ThinkingBudget)
		}
	}

	return nil
}

func formatCostPerMillion(cost *float64) string {
	if cost == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.6f", *cost)
}

// ModelSystemPromptOptions contains parameters for showing system prompts
type ModelSystemPromptOptions struct {
	Config       *config.RawConfig
	ModelName    string
	DefaultModel string // Fallback model name from env var
	Output       io.Writer
	// SystemPrompt is an optional override for testing - if provided, this file
	// is used instead of opening the file from the path in config
	SystemPrompt fs.File
}

// ModelSystemPrompt displays the rendered system prompt for a model
func ModelSystemPrompt(ctx context.Context, opts ModelSystemPromptOptions) error {
	// Determine the model to use
	modelName := opts.ModelName
	if modelName == "" {
		if opts.Config.Defaults.Model != "" {
			modelName = opts.Config.Defaults.Model
		} else if opts.DefaultModel != "" {
			modelName = opts.DefaultModel
		}
	}

	if modelName == "" {
		return fmt.Errorf("no model specified. Use --model flag or set defaults.model in configuration")
	}

	selectedModel, found := opts.Config.FindModel(modelName)
	if !found {
		return fmt.Errorf("model %q not found in configuration", modelName)
	}

	// Determine system prompt path
	systemPromptPath := opts.Config.Defaults.SystemPromptPath
	if selectedModel.SystemPromptPath != "" {
		systemPromptPath = selectedModel.SystemPromptPath
	}

	// Determine the file to read from
	var promptFile fs.File
	var shouldClose bool
	if opts.SystemPrompt != nil {
		// Use provided file (for testing)
		promptFile = opts.SystemPrompt
		shouldClose = false
	} else if systemPromptPath != "" {
		// Open file from path
		f, err := os.Open(systemPromptPath)
		if err != nil {
			return fmt.Errorf("could not open system prompt file %q: %w", systemPromptPath, err)
		}
		promptFile = f
		shouldClose = true
	}

	if promptFile == nil {
		fmt.Fprintf(opts.Output, "Model %q does not define a system prompt.\n", modelName)
		return nil
	}

	if shouldClose {
		defer promptFile.Close()
	}

	contents, err := io.ReadAll(promptFile)
	if err != nil {
		return fmt.Errorf("failed to read system prompt file: %w", err)
	}

	// Resolve effective code mode config for template rendering
	var codeMode *config.CodeModeConfig
	if selectedModel.CodeMode != nil {
		codeMode = selectedModel.CodeMode
	} else if opts.Config.Defaults.CodeMode != nil {
		codeMode = opts.Config.Defaults.CodeMode
	}

	// Create a minimal Config for template rendering
	templateConfig := &config.Config{
		Model:              selectedModel.Model,
		MCPServers:         opts.Config.MCPServers,
		GenerationDefaults: nil,
		CodeMode:           codeMode,
	}

	systemPrompt, err := agent.SystemPromptTemplate(ctx, string(contents), agent.TemplateData{
		Config: templateConfig,
	}, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to render system prompt: %w", err)
	}

	_, err = fmt.Fprintf(opts.Output, "Model: %s\nPath: %s\n\n%s\n", modelName, systemPromptPath, systemPrompt)
	return err
}
