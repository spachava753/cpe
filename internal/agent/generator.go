package agent

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v2"
	oaiopt "github.com/openai/openai-go/v2/option"
	"github.com/spachava753/cpe/internal/modelcatalog"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/cenkalti/backoff/v5"
)

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

func createOpenAIGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string) (gai.Generator, error) {
	clientOpts := []oaiopt.RequestOption{
		oaiopt.WithRequestTimeout(timeout),
	}
	if baseURL != "" {
		clientOpts = append(clientOpts, oaiopt.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		clientOpts = append(clientOpts, oaiopt.WithAPIKey(apiKey))
	}
	client := openai.NewClient(clientOpts...)
	generator := gai.NewOpenAiGenerator(&client.Chat.Completions, model, systemPrompt)
	return &generator, nil
}

func createAnthropicGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string) (gai.Generator, error) {
	var client anthropic.Client
	opts := []aopts.RequestOption{
		aopts.WithRequestTimeout(timeout),
	}
	if baseURL != "" {
		opts = append(opts, aopts.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		opts = append(opts, aopts.WithAPIKey(apiKey))
	}
	client = anthropic.NewClient(opts...)
	svc := gai.NewAnthropicServiceWrapper(&client.Messages, gai.EnableSystemCaching, gai.EnableMultiTurnCaching)
	generator := gai.NewAnthropicGenerator(svc, model, systemPrompt)
	return generator, nil
}

func createGeminiGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string) (gai.Generator, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}
	cc := genai.ClientConfig{
		APIKey:      apiKey,
		HTTPOptions: genai.HTTPOptions{BaseURL: baseURL},
	}
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &cc)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	g, err := gai.NewGeminiGenerator(client, model, systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini generator: %w", err)
	}
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 1 * time.Minute
	b.Reset()
	return gai.NewRetryGenerator(g, b, backoff.WithMaxTries(3), backoff.WithMaxElapsedTime(5*time.Minute)), nil
}

type ToolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

func InitGeneratorFromModel(m modelcatalog.Model, systemPrompt string, timeout time.Duration, overrideBaseURL string) (gai.Generator, error) {
	t := strings.ToLower(m.Type)
	baseURL := m.BaseUrl
	if overrideBaseURL != "" {
		baseURL = overrideBaseURL
	}
	apiEnv := strings.TrimSpace(m.ApiKeyEnv)
	switch t {
	case "openai":
		if apiEnv == "" {
			apiEnv = "OPENAI_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return createOpenAIGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey)
	case "anthropic":
		if apiEnv == "" {
			apiEnv = "ANTHROPIC_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return createAnthropicGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey)
	case "gemini":
		if apiEnv == "" {
			apiEnv = "GEMINI_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return createGeminiGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey)
	case "cerebras":
		if apiEnv == "" {
			apiEnv = "CEREBRAS_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return gai.NewCerebrasGenerator(nil, baseURL, m.ID, systemPrompt, apiKey), nil
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}
}
