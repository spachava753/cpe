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
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/db"
	"github.com/spachava753/cpe/internal/ignore"
)

//go:embed agent_instructions.txt
var agentInstructions string

// Executor defines the interface for executing agentic workflows
// Input represents a single input to be processed by the model
type Input struct {
	Type     InputType
	Text     string // Used when Type is tools.InputTypeText
	FilePath string // Used when Type is tools.InputTypeImage, tools.InputTypeVideo, or tools.InputTypeAudio
}

type Executor interface {
	Execute(inputs []Input) error
	LoadMessages(r io.Reader) error
	SaveMessages(w io.Writer) error
	PrintMessages() string
}

type Logger interface {
	Print(v ...any)
	Printf(format string, v ...any)
	Println(v ...any)
}

// createExecutor creates a new executor instance based on the model configuration
func createExecutor(logger Logger, ignorer *gitignore.GitIgnore, customURL string, genConfig GenConfig) (Executor, error) {
	var executor Executor
	var err error

	switch genConfig.Model {
	case a.ModelClaude3_5Sonnet20241022, a.ModelClaude3_5Haiku20241022, a.ModelClaude_3_Haiku_20240307, a.ModelClaude_3_Opus_20240229, a.ModelClaude3_7Sonnet20250219:
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		}
		executor, err = NewAnthropicExecutor(customURL, apiKey, logger, ignorer, genConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create anthropic executor: %w", err)
		}
	case "gemini-1.5-pro-002", "gemini-1.5-flash-002", "gemini-2.0-flash-exp", "gemini-2.0-flash", "gemini-2.0-flash-lite-preview-02-05", "gemini-2.0-pro-exp-02-05":
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
		executor, err = NewOpenAIExecutor(customURL, apiKey, logger, ignorer, genConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create openai executor: %w", err)
		}
	}

	return executor, nil
}

// getCustomURL returns the custom URL to use based on the following precedence:
// 1. Command-line flag (-custom-url)
// 2. Model-specific environment variable (CPE_MODEL_NAME_URL)
// 3. General custom URL environment variable (CPE_CUSTOM_URL)
func getCustomURL(flagURL string, modelName string) string {
	// Start with the flag value
	customURL := flagURL

	// Check model-specific env var if we have a model name
	if modelName != "" {
		envVarName := fmt.Sprintf("CPE_%s_URL", strings.ToUpper(strings.ReplaceAll(modelName, "-", "_")))
		if modelEnvURL := os.Getenv(envVarName); customURL == "" && modelEnvURL != "" {
			customURL = modelEnvURL
		}
	}

	// Finally, check the general custom URL env var
	if envURL := os.Getenv("CPE_CUSTOM_URL"); customURL == "" && envURL != "" {
		customURL = envURL
	}

	return customURL
}

// GetModelFromFlagsOrDefault returns the model to use based on flags, environment variable, or default
func GetModelFromFlagsOrDefault(flags ModelOptions) string {
	if flags.Model != "" {
		return flags.Model
	}
	if envModel := os.Getenv("CPE_MODEL"); envModel != "" {
		return envModel
	}
	return DefaultModel
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

	// If -new flag is supplied or no previous conversation exists, create a new executor
	if flags.New {
		flags.Model = GetModelFromFlagsOrDefault(flags)
		genConfig, err := GetConfig(flags)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}

		executor, err := createExecutor(logger, ignorer, getCustomURL(flags.CustomURL, flags.Model), genConfig)
		if err != nil {
			return nil, err
		}

		return &executorWrapper{
			executor:     executor,
			convoManager: convoManager,
			model:        genConfig.Model,
			userMessage:  "",
			continueID:   "",
		}, nil
	}

	// Get conversation from DB (either specific conversation or latest)
	var conv *db.Conversation
	if flags.Continue != "" {
		conv, err = convoManager.GetConversation(context.Background(), flags.Continue)
		if err != nil {
			return nil, fmt.Errorf("failed to get conversation: %w", err)
		}
	} else {
		conv, err = convoManager.GetLatestConversation(context.Background())
		if err != nil {
			// If no conversation exists, create new executor with default model
			flags.Model = GetModelFromFlagsOrDefault(flags)
			genConfig, err := GetConfig(flags)
			if err != nil {
				return nil, fmt.Errorf("failed to get config: %w", err)
			}

			executor, err := createExecutor(logger, ignorer, getCustomURL(flags.CustomURL, flags.Model), genConfig)
			if err != nil {
				return nil, err
			}

			return &executorWrapper{
				executor:     executor,
				convoManager: convoManager,
				model:        genConfig.Model,
				userMessage:  "",
				continueID:   "",
			}, nil
		}
	}

	// Determine which model to use (from flag or conversation)
	var genConfig GenConfig
	if flags.Model != "" {
		// Model specified in flag - verify it matches conversation
		flagConfig, ok := ModelConfigs[flags.Model]
		if !ok {
			return nil, fmt.Errorf("unknown model '%s'", flags.Model)
		}
		if flagConfig.Name != conv.Model {
			return nil, fmt.Errorf("cannot continue conversation with a different model (conversation model: %s, requested model: %s)", conv.Model, flagConfig.Name)
		}
		genConfig, err = GetConfig(flags)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
	} else {
		// Use model from conversation - find config by model name
		var modelAlias string
		var found bool
		for alias, config := range ModelConfigs {
			if config.Name == conv.Model {
				modelAlias = alias
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("cannot continue conversation: stored model '%s' is not supported", conv.Model)
		}
		genConfig, err = GetConfig(ModelOptions{Model: modelAlias})
		if err != nil {
			return nil, fmt.Errorf("failed to get config for model %s: %w", modelAlias, err)
		}

		flags.Model = modelAlias
	}

	// Create executor and load conversation state
	customURL := getCustomURL(flags.CustomURL, flags.Model)

	executor, err := createExecutor(logger, ignorer, customURL, genConfig)
	if err != nil {
		return nil, err
	}

	if err := executor.LoadMessages(bytes.NewReader(conv.ExecutorData)); err != nil {
		return nil, fmt.Errorf("failed to load messages: %w", err)
	}

	return &executorWrapper{
		executor:     executor,
		convoManager: convoManager,
		model:        genConfig.Model,
		userMessage:  "",
		continueID:   conv.ID,
	}, nil
}
