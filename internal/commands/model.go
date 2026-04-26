package commands

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/prompt"
)

// ModelListOptions contains dependencies for ModelList.
// Config must be preloaded by the caller.
type ModelListOptions struct {
	Config       *config.RawConfig
	DefaultModel string
	Writer       io.Writer
}

// ModelList prints model refs in configuration order and marks the effective
// default model when provided.
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

// ModelInfoOptions contains dependencies for ModelInfo.
type ModelInfoOptions struct {
	Config    *config.RawConfig
	ModelName string
	Writer    io.Writer
}

// ModelInfo prints details for one configured model ref.
// It reports exactly what is configured and does not apply runtime resolution
// merges from defaults/CLI flags.
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

// ModelSystemPromptOptions contains dependencies for ModelSystemPrompt.
// The caller provides raw config and optional model selection hints.
type ModelSystemPromptOptions struct {
	RawConfig      *config.RawConfig
	Config         config.Config
	ConfigFilePath string
	ModelName      string
	DefaultModel   string // Fallback model name from env var
	Output         io.Writer
	Stderr         io.Writer
	// SystemPrompt is an optional override for testing - if provided, this file
	// is used instead of opening the file from the path in config
	SystemPrompt fs.File
}

// ModelSystemPrompt resolves and renders the effective system prompt for one
// model.
//
// Model selection precedence:
//   - opts.ModelName
//   - config defaults.model
//   - opts.DefaultModel (typically CPE_MODEL)
//
// System prompt path precedence:
//   - model.systemPromptPath
//   - defaults.systemPromptPath
func ModelSystemPrompt(ctx context.Context, opts ModelSystemPromptOptions) error {
	// Determine the model to use
	modelName := opts.ModelName
	if modelName == "" {
		if opts.RawConfig.Defaults.Model != "" {
			modelName = opts.RawConfig.Defaults.Model
		} else if opts.DefaultModel != "" {
			modelName = opts.DefaultModel
		}
	}

	if modelName == "" {
		return fmt.Errorf("no model specified. Use --model flag or set defaults.model in configuration")
	}

	selectedModel, found := opts.RawConfig.FindModel(modelName)
	if !found {
		return fmt.Errorf("model %q not found in configuration", modelName)
	}

	// Determine system prompt path
	systemPromptPath := opts.RawConfig.Defaults.SystemPromptPath
	if selectedModel.SystemPromptPath != "" {
		systemPromptPath = selectedModel.SystemPromptPath
	}

	resolvedSystemPromptPath := systemPromptPath
	if resolvedSystemPromptPath != "" && !filepath.IsAbs(resolvedSystemPromptPath) && opts.ConfigFilePath != "" {
		resolvedSystemPromptPath = filepath.Join(filepath.Dir(opts.ConfigFilePath), resolvedSystemPromptPath)
	}

	// Determine the file to read from
	var promptFile fs.File
	var shouldClose bool
	if opts.SystemPrompt != nil {
		// Use provided file (for testing)
		promptFile = opts.SystemPrompt
		shouldClose = false
	} else if resolvedSystemPromptPath != "" {
		// Open file from path
		f, err := os.Open(resolvedSystemPromptPath)
		if err != nil {
			return fmt.Errorf("could not open system prompt file %q: %w", resolvedSystemPromptPath, err)
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

	templateConfig := opts.Config
	if reflect.ValueOf(templateConfig).IsZero() {
		// Resolve effective code mode config for template rendering when the caller
		// did not supply a fully resolved runtime config.
		var codeMode *config.CodeModeConfig
		if selectedModel.CodeMode != nil {
			codeMode = selectedModel.CodeMode
		} else if opts.RawConfig.Defaults.CodeMode != nil {
			codeMode = opts.RawConfig.Defaults.CodeMode
		}

		// Create a minimal Config for template rendering.
		templateConfig = config.Config{
			Model:              selectedModel.Model,
			MCPServers:         opts.RawConfig.MCPServers,
			GenerationDefaults: nil,
			CodeMode:           codeMode,
		}
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	systemPrompt, err := prompt.SystemPromptTemplate(ctx, string(contents), prompt.TemplateData{
		Config: templateConfig,
	}, stderr)
	if err != nil {
		return fmt.Errorf("failed to render system prompt: %w", err)
	}

	_, err = fmt.Fprintf(opts.Output, "Model: %s\nPath: %s\n\n%s\n", modelName, resolvedSystemPromptPath, systemPrompt)
	return err
}
