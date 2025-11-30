package commands

import (
	"context"
	"fmt"
	"io"
	"io/fs"

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

	fmt.Fprintf(opts.Writer, "Ref: %s\nDisplay Name: %s\nType: %s\nID: %s\nContext: %d\nMaxOutput: %d\nInputCostPerMillion: %.6f\nOutputCostPerMillion: %.6f\n",
		model.Ref, model.DisplayName, model.Type, model.ID, model.ContextWindow, model.MaxOutput, model.InputCostPerMillion, model.OutputCostPerMillion,
	)

	if model.GenerationDefaults != nil {
		fmt.Fprintln(opts.Writer, "\nGeneration Defaults:")
		if model.GenerationDefaults.Temperature != 0 {
			fmt.Fprintf(opts.Writer, "  Temperature: %.2f\n", model.GenerationDefaults.Temperature)
		}
		if model.GenerationDefaults.TopP != 0 {
			fmt.Fprintf(opts.Writer, "  TopP: %.2f\n", model.GenerationDefaults.TopP)
		}
		if model.GenerationDefaults.TopK != 0 {
			fmt.Fprintf(opts.Writer, "  TopK: %d\n", model.GenerationDefaults.TopK)
		}
		if model.GenerationDefaults.MaxGenerationTokens != 0 {
			fmt.Fprintf(opts.Writer, "  MaxTokens: %d\n", model.GenerationDefaults.MaxGenerationTokens)
		}
		if model.GenerationDefaults.ThinkingBudget != "" {
			fmt.Fprintf(opts.Writer, "  ThinkingBudget: %s\n", model.GenerationDefaults.ThinkingBudget)
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
	Config       *config.RawConfig
	ModelName    string
	SystemPrompt fs.File
	Output       io.Writer
}

// ModelSystemPrompt displays the rendered system prompt for a model
func ModelSystemPrompt(opts ModelSystemPromptOptions) error {
	if opts.ModelName == "" {
		return fmt.Errorf("no model specified")
	}

	selectedModel, found := opts.Config.FindModel(opts.ModelName)
	if !found {
		return fmt.Errorf("model %q not found in configuration", opts.ModelName)
	}

	if opts.SystemPrompt == nil {
		fmt.Fprintf(opts.Output, "Model %q does not define a system prompt.\n", opts.ModelName)
		return nil
	}

	contents, err := io.ReadAll(opts.SystemPrompt)
	if err != nil {
		return err
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

	systemPrompt, err := agent.SystemPromptTemplate(string(contents), agent.TemplateData{
		Config: templateConfig,
	})
	if err != nil {
		return err
	}

	systemPromptPath := selectedModel.SystemPromptPath
	if systemPromptPath == "" {
		systemPromptPath = opts.Config.Defaults.SystemPromptPath
	}

	_, err = fmt.Fprintf(opts.Output, "Model: %s\nPath: %s\n\n%s\n", opts.ModelName, systemPromptPath, systemPrompt)
	return err
}
