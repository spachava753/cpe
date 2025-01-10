package agent

import (
	_ "embed"
	"encoding/gob"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/spachava753/cpe/internal/ignore"
	"io"
	"log/slog"
	"os"
	"strings"
)

//go:embed agent_instructions.txt
var agentInstructions string

// Executor defines the interface for executing agentic workflows
type Executor interface {
	Execute(input string) error
	LoadMessages(r io.Reader) error
	SaveMessages(w io.Writer) error
}

// InitExecutor initializes and returns an appropriate executor based on the model configuration
func InitExecutor(logger *slog.Logger, flags ModelOptions) (Executor, error) {
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		return nil, fmt.Errorf("failed to load ignore files: %w", err)
	}
	if ignorer == nil {
		return nil, fmt.Errorf("git ignorer was nil")
	}

	// Check for custom URL in environment variable
	customURL := flags.CustomURL
	if modelEnvURL := os.Getenv(fmt.Sprintf("CPE_%s_URL", strings.ToUpper(strings.ReplaceAll(flags.Model, "-", "_")))); customURL == "" && modelEnvURL != "" {
		customURL = modelEnvURL
	}
	if envURL := os.Getenv("CPE_CUSTOM_URL"); customURL == "" && envURL != "" {
		customURL = envURL
	}

	genConfig, err := GetConfig(logger, flags)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	var executor Executor

	// Check if we have a specific executor for this model
	switch genConfig.Model {
	case "deepseek-chat":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable not set")
		}
		executor = NewDeepSeekExecutor(customURL, apiKey, logger, ignorer, genConfig)
	case anthropic.ModelClaude3_5Sonnet20241022, anthropic.ModelClaude3_5Haiku20241022, anthropic.ModelClaude_3_Haiku_20240307, anthropic.ModelClaude_3_Opus_20240229:
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		}
		executor = NewAnthropicExecutor(customURL, apiKey, logger, ignorer, genConfig)
	case "gemini-1.5-pro-002", "gemini-1.5-flash-002", "gemini-2.0-flash-exp":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
		}
		executor, err = NewGeminiExecutor(customURL, apiKey, logger, ignorer, genConfig)
		if err != nil {
			return nil, err
		}
	default:
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		executor = NewOpenAIExecutor(customURL, apiKey, logger, ignorer, genConfig)
	}

	// If continue flag is set, load previous messages
	if flags.Continue {
		// First decode just the Type field to check executor compatibility
		f, err := os.Open(".cpeconvo")
		if err != nil {
			return nil, fmt.Errorf("failed to open conversation file: %w", err)
		}
		defer f.Close()

		var convo struct {
			Type string
		}
		dec := gob.NewDecoder(f)
		if err := dec.Decode(&convo); err != nil {
			return nil, fmt.Errorf("failed to decode conversation type: %w", err)
		}

		// Check if the previous executor type matches the current one
		var executorType string
		switch genConfig.Model {
		case "deepseek-chat":
			executorType = "deepseek"
		case anthropic.ModelClaude3_5Sonnet20241022, anthropic.ModelClaude3_5Haiku20241022, anthropic.ModelClaude_3_Haiku_20240307, anthropic.ModelClaude_3_Opus_20240229:
			executorType = "anthropic"
		case "gemini-1.5-pro-002", "gemini-1.5-flash-002", "gemini-2.0-flash-exp":
			executorType = "gemini"
		default:
			executorType = "openai"
		}

		if convo.Type != executorType {
			return nil, fmt.Errorf("cannot continue conversation: previous conversation was with %s executor, but current executor is %s", convo.Type, executorType)
		}

		// Reopen file to read from beginning
		if _, err := f.Seek(0, 0); err != nil {
			return nil, fmt.Errorf("failed to seek to beginning of file: %w", err)
		}

		// Load messages into executor
		if err := executor.LoadMessages(f); err != nil {
			return nil, fmt.Errorf("failed to load messages: %w", err)
		}
	}

	return executor, nil
}
