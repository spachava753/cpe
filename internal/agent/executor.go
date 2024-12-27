package agent

import (
	_ "embed"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/spachava753/cpe/internal/ignore"
	"log/slog"
	"os"
)

//go:embed agent_instructions.txt
var agentInstructions string

// Executor defines the interface for executing agentic workflows
type Executor interface {
	Execute(input string) error
}

// InitExecutor initializes and returns an appropriate executor based on the model configuration
func InitExecutor(logger *slog.Logger, modelName string, flags ModelOptions) (Executor, error) {
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		return nil, fmt.Errorf("failed to load ignore files: %w", err)
	}
	if ignorer == nil {
		return nil, fmt.Errorf("git ignorer was nil")
	}

	// Check for custom URL in environment variable
	customURL := flags.CustomURL
	if envURL := os.Getenv("CPE_CUSTOM_URL"); customURL == "" && envURL != "" {
		customURL = envURL
	}

	genConfig, err := GetConfig(logger, modelName, flags)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	// Check if we have a specific executor for this model
	switch genConfig.Model {
	case "deepseek-chat":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable not set")
		}
		return NewDeepSeekExecutor(customURL, apiKey, logger, ignorer, genConfig), nil
	case anthropic.ModelClaude3_5Sonnet20241022, anthropic.ModelClaude3_5Haiku20241022, anthropic.ModelClaude_3_Haiku_20240307, anthropic.ModelClaude_3_Opus_20240229:
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		}
		return NewAnthropicExecutor(customURL, apiKey, logger, ignorer, genConfig), nil
	case "gemini-1.5-pro-002", "gemini-1.5-flash-002", "gemini-2.0-flash-exp":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
		}
		return NewGeminiExecutor(customURL, apiKey, logger, ignorer, genConfig)
	default:
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		return NewOpenAIExecutor(customURL, apiKey, logger, ignorer, genConfig), nil
	}
}
