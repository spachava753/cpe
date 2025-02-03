package agent

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	defer convoManager.Close()

	// Handle conversation management commands
	if flags.ListConversations {
		conversations, err := convoManager.ListConversations(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to list conversations: %w", err)
		}

		// Print table header
		fmt.Printf("%-24s %-24s %-15s %-25s %s\n", "ID", "Parent ID", "Model", "Created At", "Message")
		fmt.Println(strings.Repeat("-", 120))

		// Print each conversation
		for _, conv := range conversations {
			parentID := "-"
			if conv.ParentID.Valid {
				parentID = conv.ParentID.String
			}
			// Truncate user message if too long
			message := conv.UserMessage
			if len(message) > 50 {
				message = message[:47] + "..."
			}
			fmt.Printf("%-24s %-24s %-15s %-25s %s\n",
				conv.ID,
				parentID,
				conv.Model,
				conv.CreatedAt.Format("2006-01-02 15:04:05"),
				message,
			)
		}
		return nil, nil // Exit after listing conversations
	}

	if flags.DeleteConversation != "" {
		if err := convoManager.DeleteConversation(context.Background(), flags.DeleteConversation, flags.DeleteCascade); err != nil {
			return nil, fmt.Errorf("failed to delete conversation: %w", err)
		}
		return nil, nil // Exit after deleting conversation
	}

	if flags.PrintConversation != "" {
		conv, err := convoManager.GetConversation(context.Background(), flags.PrintConversation)
		if err != nil {
			return nil, fmt.Errorf("failed to get conversation: %w", err)
		}
		fmt.Printf("Conversation ID: %s\n", conv.ID)
		if conv.ParentID.Valid {
			fmt.Printf("Parent ID: %s\n", conv.ParentID.String)
		}
		fmt.Printf("Model: %s\n", conv.Model)
		fmt.Printf("Created At: %s\n", conv.CreatedAt.Format(time.RFC3339))
		fmt.Printf("\nUser Message:\n%s\n", conv.UserMessage)
		return nil, nil // Exit after printing conversation
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
	if flags.Continue != "" {
		var conv *db.Conversation
		var err error

		if flags.Continue == "last" {
			conv, err = convoManager.GetLatestConversation(context.Background())
		} else {
			conv, err = convoManager.GetConversation(context.Background(), flags.Continue)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get conversation: %w", err)
		}

		// Verify model compatibility
		if conv.Model != genConfig.Model {
			return nil, fmt.Errorf("cannot continue conversation from a different executor (conversation model: %s, requested model: %s)", conv.Model, genConfig.Model)
		}

		// Load messages into executor
		if err := executor.LoadMessages(bytes.NewReader(conv.ExecutorData)); err != nil {
			return nil, fmt.Errorf("failed to load messages: %w", err)
		}
	}

	return &executorWrapper{
		executor:     executor,
		convoManager: convoManager,
		model:        genConfig.Model,
		userMessage:  flags.Input,
		continueID:   flags.Continue,
	}, nil
}
