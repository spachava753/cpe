package agent

import (
	_ "embed"
	"github.com/anthropics/anthropic-sdk-go"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/llm"
	"log/slog"
)

//go:embed agent_instructions.txt
var agentInstructions string

// Executor defines the interface for executing agentic workflows
type Executor interface {
	Execute(input string) error
}

// NewExecutor creates a new executor based on the model and configuration
func NewExecutor(baseUrl string, provider llm.LLMProvider, genConfig llm.GenConfig, logger *slog.Logger, ignorer *gitignore.GitIgnore) (Executor, error) {
	// Check if we have a specific executor for this model
	switch genConfig.Model {
	case anthropic.ModelClaude3_5Sonnet20241022:
		// TODO: there seems to be an error in the anthropic api, holding off on enabling sonnet specific executor until issue is resolve: https://github.com/anthropics/anthropic-sdk-go/issues/86
		fallthrough
		//apiKey := os.Getenv("ANTHROPIC_API_KEY")
		//if apiKey == "" {
		//	return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		//}
		//return NewSonnet35Executor(baseUrl, apiKey, logger, ignorer, genConfig), nil
	default:
		// Use generic executor for all other models
		return NewGenericExecutor(provider, genConfig, logger, ignorer), nil
	}
}
