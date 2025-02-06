package agent

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"

	a "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/db"
	"github.com/spachava753/cpe/internal/ignore"
)

//go:embed agent_instructions.txt
var agentInstructions string

//go:embed reasoning_agent_instructions.txt
var reasoningAgentInstructions string

// Executor defines the interface for executing agentic workflows
type Executor interface {
	Execute(input string) error
	LoadMessages(r io.Reader) error
	SaveMessages(w io.Writer) error
	PrintMessages() string
}

type Logger interface {
	Print(v ...any)
	Printf(format string, v ...any)
	Println(v ...any)
}

// InitExecutor initializes and returns an appropriate executor based on the model configuration
func InitExecutor(logger Logger, flags ModelOptions) (Executor, error) {
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		return nil, fmt.Errorf("failed to load ignore files: %w", err)
	}
	if ignorer == nil {
		return nil, fmt.Errorf("git ignorer was nil")
	}

	// Initialize conversation manager
	dbPath := ".cpeconvo"
	convoManager, err := conversation.NewManager(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize conversation manager: %w", err)
	}

	// Check for custom URL in environment variable
	customURL := flags.CustomURL
	if modelEnvURL := os.Getenv(fmt.Sprintf("CPE_%s_URL", strings.ToUpper(strings.ReplaceAll(flags.Model, "-", "_")))); customURL == "" && modelEnvURL != "" {
		customURL = modelEnvURL
	}
	if envURL := os.Getenv("CPE_CUSTOM_URL"); customURL == "" && envURL != "" {
		customURL = envURL
	}

	genConfig, err := GetConfig(flags)
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
	case "deepseek-reasoner":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable not set")
		}
		if customURL == "" {
			customURL = "https://api.deepseek.com/"
		}
		executor = NewOpenAiReasoningExecutor(customURL, apiKey, logger, ignorer, genConfig)
	case openai.ChatModelO1Preview, openai.ChatModelO1Preview2024_09_12, openai.ChatModelO1Mini, openai.ChatModelO1Mini2024_09_12:
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		executor = NewOpenAiReasoningExecutor(customURL, apiKey, logger, ignorer, genConfig)
	case a.ModelClaude3_5Sonnet20241022, a.ModelClaude3_5Haiku20241022, a.ModelClaude_3_Haiku_20240307, a.ModelClaude_3_Opus_20240229:
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
	var continueId string
	if flags.Continue != "" {
		var conv *db.Conversation
		var err error

		conv, err = convoManager.GetConversation(context.Background(), flags.Continue)
		if err != nil {
			return nil, fmt.Errorf("failed to get conversation: %w", err)
		}

		continueId = conv.ID

		// Verify model compatibility
		if conv.Model != genConfig.Model {
			return nil, fmt.Errorf("cannot continue conversation from a different executor (conversation model: %s, requested model: %s)", conv.Model, genConfig.Model)
		}

		// Load messages into executor
		if err := executor.LoadMessages(bytes.NewReader(conv.ExecutorData)); err != nil {
			return nil, fmt.Errorf("failed to load messages: %w", err)
		}
	} else if !flags.New {
		// Try to continue from latest conversation if one exists
		conv, err := convoManager.GetLatestConversation(context.Background())
		if err == nil {
			continueId = conv.ID

			// Verify model compatibility
			if conv.Model != genConfig.Model {
				return nil, fmt.Errorf("cannot continue conversation from a different executor (conversation model: %s, requested model: %s)", conv.Model, genConfig.Model)
			}

			// Load messages into executor
			if err := executor.LoadMessages(bytes.NewReader(conv.ExecutorData)); err != nil {
				return nil, fmt.Errorf("failed to load messages: %w", err)
			}
		}
	}

	return &executorWrapper{
		executor:     executor,
		convoManager: convoManager,
		model:        genConfig.Model,
		userMessage:  flags.Input,
		continueID:   continueId,
	}, nil
}
