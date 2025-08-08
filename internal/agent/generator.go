package agent

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v2"
	oaiopt "github.com/openai/openai-go/v2/option"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/cenkalti/backoff/v5"
)

var geminiModels = []string{
	"gemini-2.5-pro",
	"gemini-2.5-pro-preview-06-05",
	"gemini-2.5-pro-preview-05-06",
	"gemini-2.5-pro-preview-03-25",
	"gemini-2.5-flash",
	"gemini-2.5-flash-preview-05-20",
	"gemini-2.5-flash-preview-04-17",
	"gemini-2.5-flash-lite-preview-06-17",
	"gemini-2.0-flash",
	"gemini-2.0-flash-lite",
	"gemini-1.5-pro",
	"gemini-1.5-flash",
	"gemini-1.5-flash-8b",
	"gemma-3-27b-it",
	"gemma-3-12b-it",
	"gemma-3-4b-it",
	"gemma-3-1b-it",
}

var anthropicModels = []string{
	string(anthropic.ModelClaude3_7SonnetLatest),
	string(anthropic.ModelClaude3_7Sonnet20250219),
	string(anthropic.ModelClaude3_5HaikuLatest),
	string(anthropic.ModelClaude3_5Haiku20241022),
	string(anthropic.ModelClaude3_5SonnetLatest),
	string(anthropic.ModelClaude3_5Sonnet20241022),
	string(anthropic.ModelClaude_3_5_Sonnet_20240620),
	string(anthropic.ModelClaudeOpus4_0),
	string(anthropic.ModelClaude4Opus20250514),
	string(anthropic.ModelClaudeSonnet4_0),
	string(anthropic.ModelClaude4Sonnet20250514),
	string(anthropic.ModelClaudeOpus4_1_20250805),
}

var openAiModels = []string{
	openai.ChatModelGPT5,
	openai.ChatModelGPT5_2025_08_07,
	openai.ChatModelGPT5Mini,
	openai.ChatModelGPT5Mini2025_08_07,
	openai.ChatModelGPT5Nano,
	openai.ChatModelGPT5Nano2025_08_07,
	openai.ChatModelO3,
	openai.ChatModelO4Mini,
	openai.ChatModelGPT4_1,
	openai.ChatModelGPT4_1Mini,
	openai.ChatModelGPT4_1Nano,
	openai.ChatModelGPT4_1_2025_04_14,
	openai.ChatModelGPT4_1Mini2025_04_14,
	openai.ChatModelGPT4_1Nano2025_04_14,
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

var KnownModels = slices.Concat(openAiModels, anthropicModels, geminiModels)

//go:embed agent_instructions.txt
var agentInstructions string

// PrepareSystemPrompt prepares the system prompt from either a custom file or the default template
func PrepareSystemPrompt(systemPromptPath string) (string, error) {
	// Get system information for template execution
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return "", fmt.Errorf("failed to get system info: %w", err)
	}

	// Get agent instructions with system info
	var systemPrompt string
	if systemPromptPath != "" {
		// User provided a custom template file
		systemPrompt, err = sysInfo.ExecuteTemplate(systemPromptPath)
		if err != nil {
			return "", fmt.Errorf("failed to execute custom system prompt template: %w", err)
		}
	} else {
		// Use the default template
		systemPrompt, err = sysInfo.ExecuteTemplateString(agentInstructions)
		if err != nil {
			return "", fmt.Errorf("failed to execute default system prompt template: %w", err)
		}
	}

	return systemPrompt, nil
}

// InitGenerator creates the appropriate generator based on the model name
// without any printing wrappers
func InitGenerator(model, baseURL, systemPrompt string, timeout time.Duration) (gai.Generator, error) {
	var generator gai.Generator
	var err error

	// Handle OpenAI models
	if slices.Contains(openAiModels, model) {
		generator, err = createOpenAIGenerator(model, baseURL, systemPrompt, timeout)
		if err != nil {
			return nil, err
		}
	} else if slices.Contains(anthropicModels, model) {
		// Handle Anthropic models
		generator, err = createAnthropicGenerator(model, baseURL, systemPrompt, timeout)
		if err != nil {
			return nil, err
		}
	} else if slices.Contains(geminiModels, model) {
		// Handle Gemini models
		generator, err = createGeminiGenerator(model, baseURL, systemPrompt, timeout)
		if err != nil {
			return nil, err
		}
	} else {
		// Custom openai-compatible models require a base url
		if baseURL == "" {
			return nil, fmt.Errorf("unknown model: %s, must specfiy a custom url", model)
		}

		// custom model
		generator, err = createOpenAIGenerator(model, baseURL, systemPrompt, timeout)
		if err != nil {
			return nil, err
		}
	}

	return generator, nil
}

// createOpenAIGenerator creates and configures an OpenAI generator
func createOpenAIGenerator(model, baseURL, systemPrompt string, timeout time.Duration) (gai.Generator, error) {
	clientOpts := []oaiopt.RequestOption{
		oaiopt.WithRequestTimeout(timeout),
	}

	// Create OpenAI client
	var client openai.Client
	if baseURL != "" {
		clientOpts = append(clientOpts, oaiopt.WithBaseURL(baseURL))
	}

	client = openai.NewClient(
		clientOpts...,
	)

	// Create the OpenAI generator
	generator := gai.NewOpenAiGenerator(
		&client.Chat.Completions,
		model,
		systemPrompt,
	)

	return &generator, nil
}

// createAnthropicGenerator creates and configures an Anthropic generator
func createAnthropicGenerator(model, baseURL, systemPrompt string, timeout time.Duration) (gai.Generator, error) {
	// Create Anthropic client
	var client anthropic.Client
	opts := []aopts.RequestOption{
		// Set the request timeout
		aopts.WithRequestTimeout(timeout),
	}
	if baseURL != "" {
		opts = append(
			opts,
			aopts.WithBaseURL(baseURL), // If a custom baseURL is provided, we use that
		)
	}

	client = anthropic.NewClient(opts...)

	svc := gai.NewAnthropicServiceWrapper(&client.Messages, gai.EnableSystemCaching, gai.EnableMultiTurnCaching)

	// Create and return the Anthropic generator
	generator := gai.NewAnthropicGenerator(
		svc,
		model,
		systemPrompt,
	)

	return generator, nil
}

// createGeminiGenerator creates and configures a Gemini generator
func createGeminiGenerator(model, baseURL, systemPrompt string, timeout time.Duration) (gai.Generator, error) {
	// Create Gemini client
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}

	cc := genai.ClientConfig{
		APIKey: apiKey,
		HTTPOptions: genai.HTTPOptions{
			BaseURL: baseURL,
		},
	}

	ctx := context.Background()
	client, err := genai.NewClient(
		ctx,
		&cc,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	// Create and return the Gemini generator
	g, err := gai.NewGeminiGenerator(client, model, systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini generator: %w", err)
	}
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 1 * time.Minute
	b.Reset()
	return gai.NewRetryGenerator(
		g,
		b,
		backoff.WithMaxTries(3),
		backoff.WithMaxElapsedTime(5*time.Minute),
	), nil
}

type ToolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}
