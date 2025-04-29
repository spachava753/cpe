package agent

import (
	_ "embed"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	oaiopt "github.com/openai/openai-go/option"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spachava753/cpe/internal/ignore"
	"github.com/spachava753/gai"
	"slices"
	"time"
)

var anthropicModels = []string{
	anthropic.ModelClaude3_7SonnetLatest,
	anthropic.ModelClaude3_7Sonnet20250219,
	anthropic.ModelClaude3_5HaikuLatest,
	anthropic.ModelClaude3_5Haiku20241022,
	anthropic.ModelClaude3_5SonnetLatest,
	anthropic.ModelClaude3_5Sonnet20241022,
	anthropic.ModelClaude_3_5_Sonnet_20240620,
	anthropic.ModelClaude3OpusLatest,
	anthropic.ModelClaude_3_Opus_20240229,
}

var openAiModels = []string{
	"o3",
	"o4-mini",
	"gpt-4.1",
	"gpt-4.1-mini",
	"gpt-4.1-nano",
	"gpt-4.1-2025-04-14",
	"gpt-4.1-mini-2025-04-14",
	"gpt-4.1-nano--2025-04-14",
	openai.ChatModelO3Mini,
	openai.ChatModelO3Mini2025_01_31,
	openai.ChatModelO1,
	openai.ChatModelO1_2024_12_17,
	openai.ChatModelO1Preview,
	openai.ChatModelO1Preview2024_09_12,
	openai.ChatModelO1Mini,
	openai.ChatModelO1Mini2024_09_12,
	openai.ChatModelGPT4o,
	openai.ChatModelGPT4o2024_11_20,
	openai.ChatModelGPT4o2024_08_06,
	openai.ChatModelGPT4o2024_05_13,
	openai.ChatModelGPT4oAudioPreview,
	openai.ChatModelGPT4oAudioPreview2024_10_01,
	openai.ChatModelGPT4oAudioPreview2024_12_17,
	openai.ChatModelGPT4oMiniAudioPreview,
	openai.ChatModelGPT4oMiniAudioPreview2024_12_17,
	openai.ChatModelChatgpt4oLatest,
	openai.ChatModelGPT4oMini,
	openai.ChatModelGPT4oMini2024_07_18,
	openai.ChatModelGPT4Turbo,
	openai.ChatModelGPT4Turbo2024_04_09,
	openai.ChatModelGPT4_0125Preview,
	openai.ChatModelGPT4TurboPreview,
	openai.ChatModelGPT4_1106Preview,
	openai.ChatModelGPT4VisionPreview,
	openai.ChatModelGPT4,
	openai.ChatModelGPT4_0314,
	openai.ChatModelGPT4_0613,
	openai.ChatModelGPT4_32k,
	openai.ChatModelGPT4_32k0314,
	openai.ChatModelGPT4_32k0613,
	openai.ChatModelGPT3_5Turbo,
	openai.ChatModelGPT3_5Turbo16k,
	openai.ChatModelGPT3_5Turbo0301,
	openai.ChatModelGPT3_5Turbo0613,
	openai.ChatModelGPT3_5Turbo1106,
	openai.ChatModelGPT3_5Turbo0125,
	openai.ChatModelGPT3_5Turbo16k0613,
}

var KnownModels = append(anthropicModels, openAiModels...)

//go:embed agent_instructions.txt
var agentInstructions string

// InitGenerator creates the appropriate generator based on the model name
func InitGenerator(model, baseURL, systemPromptPath string) (gai.ToolCapableGenerator, error) {
	// Handle OpenAI models
	if slices.Contains(openAiModels, model) {
		generator, err := createOpenAIGenerator(model, baseURL, systemPromptPath)
		if err != nil {
			return nil, err
		}
		// Convert to ToolCapableGenerator
		return generator.(gai.ToolCapableGenerator), nil
	}

	// Handle Anthropic models
	if slices.Contains(anthropicModels, model) {
		generator, err := createAnthropicGenerator(model, baseURL, systemPromptPath)
		if err != nil {
			return nil, err
		}
		// Convert to ToolCapableGenerator
		return generator.(gai.ToolCapableGenerator), nil
	}

	// Custom openai-compatible models require a base url
	if baseURL == "" {
		return nil, fmt.Errorf("unknown model: %s, must specfiy a custom url", model)
	}

	// custom model
	generator, err := createOpenAIGenerator(model, baseURL, systemPromptPath)
	if err != nil {
		return nil, err
	}
	// Convert to ToolCapableGenerator
	return generator.(gai.ToolCapableGenerator), nil
}

// createOpenAIGenerator creates and configures an OpenAI generator
func createOpenAIGenerator(model, baseURL, systemPromptPath string) (gai.Generator, error) {
	// Create OpenAI client
	var client openai.Client
	if baseURL != "" {
		client = openai.NewClient(oaiopt.WithBaseURL(baseURL))
	} else {
		client = openai.NewClient()
	}

	// Get system instructions
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	var systemPrompt string
	if systemPromptPath != "" {
		// User provided a custom template file
		systemPrompt, err = sysInfo.ExecuteTemplate(systemPromptPath)
		if err != nil {
			return nil, fmt.Errorf("failed to execute custom system prompt template: %w", err)
		}
	} else {
		// Use the default template
		systemPrompt, err = sysInfo.ExecuteTemplateString(agentInstructions)
		if err != nil {
			return nil, fmt.Errorf("failed to execute default system prompt template: %w", err)
		}
	}

	// Create the OpenAI generator
	generator := gai.NewOpenAiGenerator(
		&client.Chat.Completions,
		model,
		systemPrompt,
	)

	return &generator, nil
}

// createAnthropicGenerator creates and configures an Anthropic generator
func createAnthropicGenerator(model, baseURL, systemPromptPath string) (gai.Generator, error) {
	// Create Anthropic client
	var client anthropic.Client
	opts := []aopts.RequestOption{
		// Add a custom timeout to disable to the error returned if a
		// non-streaming request is expected to be above roughly 10 minutes long
		aopts.WithRequestTimeout(25 * time.Minute),
	}
	if baseURL != "" {
		opts = append(opts, aopts.WithBaseURL(baseURL))
	}

	client = anthropic.NewClient(opts...)

	svc := gai.NewAnthropicServiceWrapper(&client.Messages, gai.EnableSystemCaching, gai.EnableMultiTurnCaching)

	// Get system instructions
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	var systemPrompt string
	if systemPromptPath != "" {
		// User provided a custom template file
		systemPrompt, err = sysInfo.ExecuteTemplate(systemPromptPath)
		if err != nil {
			return nil, fmt.Errorf("failed to execute custom system prompt template: %w", err)
		}
	} else {
		// Use the default template
		systemPrompt, err = sysInfo.ExecuteTemplateString(agentInstructions)
		if err != nil {
			return nil, fmt.Errorf("failed to execute default system prompt template: %w", err)
		}
	}

	// Create and return the Anthropic generator
	generator := gai.NewAnthropicGenerator(
		svc,
		model,
		systemPrompt,
	)

	return generator, nil
}

type ToolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

// createIgnorer creates an ignorer for file system operations
func createIgnorer() (*gitignore.GitIgnore, error) {
	// Use the existing ignore.LoadIgnoreFiles function to create an ignorer
	ignorer, err := ignore.LoadIgnoreFiles(".")
	if err != nil {
		return nil, fmt.Errorf("failed to load ignore files: %w", err)
	}
	if ignorer == nil {
		return nil, fmt.Errorf("git ignorer was nil")
	}
	return ignorer, nil
}

// RegisterTools registers all the necessary tools with a tool registerer
func RegisterTools(toolRegisterer ToolRegisterer) error {
	// Create ignorer for file system tools
	ignorer, err := createIgnorer()
	if err != nil {
		return fmt.Errorf("failed to create ignorer: %w", err)
	}

	// Register bash tool
	if err := toolRegisterer.Register(bashTool, gai.ToolCallBackFunc[bashToolInput](executeBashTool)); err != nil {
		return err
	}

	// Register files overview tool
	if err := toolRegisterer.Register(filesOverviewTool, CreateExecuteFilesOverviewFunc(ignorer)); err != nil {
		return err
	}

	// Register get related files tool
	if err := toolRegisterer.Register(getRelatedFilesTool, CreateExecuteGetRelatedFilesFunc(ignorer)); err != nil {
		return err
	}

	// Register file operation tools
	if err := toolRegisterer.Register(createFileTool, gai.ToolCallBackFunc[CreateFileInput](ExecuteCreateFile)); err != nil {
		return err
	}
	if err := toolRegisterer.Register(deleteFileTool, gai.ToolCallBackFunc[DeleteFileInput](ExecuteDeleteFile)); err != nil {
		return err
	}
	if err := toolRegisterer.Register(editFileTool, gai.ToolCallBackFunc[EditFileInput](ExecuteEditFile)); err != nil {
		return err
	}
	if err := toolRegisterer.Register(moveFileTool, gai.ToolCallBackFunc[MoveFileInput](ExecuteMoveFile)); err != nil {
		return err
	}
	if err := toolRegisterer.Register(viewFileTool, gai.ToolCallBackFunc[ViewFileInput](ExecuteViewFile)); err != nil {
		return err
	}

	// Register folder operation tools
	if err := toolRegisterer.Register(createFolderTool, gai.ToolCallBackFunc[CreateFolderInput](ExecuteCreateFolder)); err != nil {
		return err
	}
	if err := toolRegisterer.Register(deleteFolderTool, gai.ToolCallBackFunc[DeleteFolderInput](ExecuteDeleteFolder)); err != nil {
		return err
	}
	if err := toolRegisterer.Register(moveFolderTool, gai.ToolCallBackFunc[MoveFolderInput](ExecuteMoveFolder)); err != nil {
		return err
	}

	return nil
}
