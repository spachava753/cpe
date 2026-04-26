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
// It reports exactly what is configured and does not apply CLI runtime overrides.
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

	if model.GenerationParams != nil {
		fmt.Fprintln(opts.Writer, "\nGeneration Params:")
		if model.GenerationParams.Temperature != nil {
			fmt.Fprintf(opts.Writer, "  Temperature: %.2f\n", *model.GenerationParams.Temperature)
		}
		if model.GenerationParams.TopP != nil {
			fmt.Fprintf(opts.Writer, "  TopP: %.2f\n", *model.GenerationParams.TopP)
		}
		if model.GenerationParams.TopK != nil {
			fmt.Fprintf(opts.Writer, "  TopK: %d\n", *model.GenerationParams.TopK)
		}
		if model.GenerationParams.MaxGenerationTokens != nil {
			fmt.Fprintf(opts.Writer, "  MaxTokens: %d\n", *model.GenerationParams.MaxGenerationTokens)
		}
		if model.GenerationParams.ThinkingBudget != "" {
			fmt.Fprintf(opts.Writer, "  ThinkingBudget: %s\n", model.GenerationParams.ThinkingBudget)
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

// ModelSystemPrompt renders the selected model profile's effective system prompt.
func ModelSystemPrompt(ctx context.Context, opts ModelSystemPromptOptions) error {
	modelName := opts.ModelName
	if modelName == "" {
		modelName = opts.DefaultModel
	}
	if modelName == "" {
		return fmt.Errorf("no model specified. Set CPE_MODEL or pass --model")
	}

	selectedModel, found := opts.RawConfig.FindModel(modelName)
	if !found {
		return fmt.Errorf("model %q not found in configuration", modelName)
	}

	resolvedSystemPromptPath := opts.Config.SystemPromptPath
	if resolvedSystemPromptPath == "" && selectedModel.SystemPromptPath != "" {
		resolvedSystemPromptPath = selectedModel.SystemPromptPath
		if !filepath.IsAbs(resolvedSystemPromptPath) && opts.ConfigFilePath != "" {
			resolvedSystemPromptPath = filepath.Join(filepath.Dir(opts.ConfigFilePath), resolvedSystemPromptPath)
		}
	}

	var promptFile fs.File
	var shouldClose bool
	if opts.SystemPrompt != nil {
		promptFile = opts.SystemPrompt
	} else if resolvedSystemPromptPath != "" {
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
		templateConfig = config.Config{
			Model:            selectedModel.Model,
			MCPServers:       selectedModel.MCPServers,
			GenerationParams: nil,
			CodeMode:         selectedModel.CodeMode,
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
