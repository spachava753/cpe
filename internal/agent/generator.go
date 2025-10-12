package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v2"
	oaiopt "github.com/openai/openai-go/v2/option"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/cenkalti/backoff/v5"
	mcpinternal "github.com/spachava753/cpe/internal/mcp"
)

// PrepareSystemPrompt prepares the system prompt from either a custom file or the default template
func PrepareSystemPrompt(systemPromptPath string, model *config.Model) (string, error) {
	// If no system prompt path is provided, return empty string
	if systemPromptPath == "" {
		return "", nil
	}

	// Get system information for template execution
	sysInfo, err := GetSystemInfoWithModel(model)
	if err != nil {
		return "", fmt.Errorf("failed to get system info: %w", err)
	}

	// Execute the custom template file
	systemPrompt, err := sysInfo.ExecuteTemplate(systemPromptPath)
	if err != nil {
		return "", fmt.Errorf("failed to execute custom system prompt template: %w", err)
	}

	return systemPrompt, nil
}

func createOpenAIGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string, patchConfig *config.PatchRequestConfig) (gai.Generator, error) {
	clientOpts := []oaiopt.RequestOption{
		oaiopt.WithRequestTimeout(timeout),
	}
	if baseURL != "" {
		clientOpts = append(clientOpts, oaiopt.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		clientOpts = append(clientOpts, oaiopt.WithAPIKey(apiKey))
	}

	if patchConfig != nil {
		transport, err := BuildPatchTransportFromConfig(nil, patchConfig)
		if err != nil {
			return nil, fmt.Errorf("building patch transport: %w", err)
		}
		clientOpts = append(clientOpts, oaiopt.WithHTTPClient(&http.Client{Transport: transport}))
	}

	client := openai.NewClient(clientOpts...)
	generator := gai.NewOpenAiGenerator(&client.Chat.Completions, model, systemPrompt)
	return &generator, nil
}

func createAnthropicGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string, patchConfig *config.PatchRequestConfig) (gai.Generator, error) {
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

	if patchConfig != nil {
		transport, err := BuildPatchTransportFromConfig(nil, patchConfig)
		if err != nil {
			return nil, fmt.Errorf("building patch transport: %w", err)
		}
		opts = append(opts, aopts.WithHTTPClient(&http.Client{Transport: transport}))
	}

	client = anthropic.NewClient(opts...)
	svc := gai.NewAnthropicServiceWrapper(&client.Messages, gai.EnableSystemCaching, gai.EnableMultiTurnCaching)
	generator := gai.NewAnthropicGenerator(svc, model, systemPrompt)
	return generator, nil
}

func createGeminiGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string, patchConfig *config.PatchRequestConfig) (gai.Generator, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}
	cc := genai.ClientConfig{
		APIKey:      apiKey,
		HTTPOptions: genai.HTTPOptions{BaseURL: baseURL},
	}

	if patchConfig != nil {
		transport, err := BuildPatchTransportFromConfig(nil, patchConfig)
		if err != nil {
			return nil, fmt.Errorf("building patch transport: %w", err)
		}
		cc.HTTPClient = &http.Client{Transport: transport}
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

func createResponsesGenerator(model, baseURL, systemPrompt string, timeout time.Duration, apiKey string, patchConfig *config.PatchRequestConfig) (gai.Generator, error) {
	clientOpts := []oaiopt.RequestOption{
		oaiopt.WithRequestTimeout(timeout),
	}
	if baseURL != "" {
		clientOpts = append(clientOpts, oaiopt.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		clientOpts = append(clientOpts, oaiopt.WithAPIKey(apiKey))
	}

	if patchConfig != nil {
		transport, err := BuildPatchTransportFromConfig(nil, patchConfig)
		if err != nil {
			return nil, fmt.Errorf("building patch transport: %w", err)
		}
		clientOpts = append(clientOpts, oaiopt.WithHTTPClient(&http.Client{Transport: transport}))
	}

	client := openai.NewClient(clientOpts...)
	generator := gai.NewResponsesGenerator(&client.Responses, model, systemPrompt)
	return &generator, nil
}

type ToolRegisterer interface {
	Register(tool gai.Tool, callback gai.ToolCallback) error
}

func InitGeneratorFromModel(m config.Model, systemPrompt string, timeout time.Duration, overrideBaseURL string) (gai.Generator, error) {
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
		return createOpenAIGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey, m.PatchRequest)
	case "anthropic":
		if apiEnv == "" {
			apiEnv = "ANTHROPIC_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return createAnthropicGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey, m.PatchRequest)
	case "gemini":
		if apiEnv == "" {
			apiEnv = "GEMINI_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return createGeminiGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey, m.PatchRequest)
	case "cerebras":
		if apiEnv == "" {
			apiEnv = "CEREBRAS_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return gai.NewCerebrasGenerator(nil, baseURL, m.ID, systemPrompt, apiKey), nil
	case "responses":
		if apiEnv == "" {
			apiEnv = "OPENAI_API_KEY"
		}
		apiKey := os.Getenv(apiEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		return createResponsesGenerator(m.ID, baseURL, systemPrompt, timeout, apiKey, m.PatchRequest)
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}
}

// CreateToolCapableGenerator creates a DialogGenerator with all middleware properly configured
func CreateToolCapableGenerator(
	ctx context.Context,
	selectedModel config.Model,
	systemPrompt string,
	requestTimeout time.Duration,
	baseURLOverride string,
	disableStreaming bool,
	mcpServers map[string]mcp.ServerConfig,
) (DialogGenerator, error) {
	// Create the base generator from catalog model
	genBase, err := InitGeneratorFromModel(selectedModel, systemPrompt, requestTimeout, baseURLOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to create generator: %w", err)
	}

	// Check if the generator supports streaming and if streaming is enabled
	var gen gai.ToolCapableGenerator
	if streamingGen, ok := genBase.(gai.StreamingGenerator); ok && !disableStreaming {
		streamingPrinter := NewStreamingPrinterGenerator(streamingGen)
		gen = &gai.StreamingAdapter{S: streamingPrinter}
	} else {
		// responses type generators need to be wrapped
		if r, ok := genBase.(*gai.ResponsesGenerator); ok {
			genBase = gai.NewResponsesToolGeneratorAdapter(*r, "")
		}
		// print non-streaming responses
		gen = NewResponsePrinterGenerator(genBase.(gai.ToolCapableGenerator))
	}

	// print token usage at the end of each message
	gen = NewTokenUsagePrinterGenerator(gen)

	// Wrap non-streaming generators with a retry wrapper so Generate is retried on transient failures
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 1 * time.Minute
	b.Reset()

	gen = gai.NewRetryGenerator(gen, b, backoff.WithMaxTries(3), backoff.WithMaxElapsedTime(5*time.Minute))

	// Create the tool generator using the printing-enabled generator
	toolGen := &gai.ToolGenerator{
		G: gen,
	}

	// Wrap the tool generator with BlockWhitelistFilter to filter thinking blocks
	// only from the initial dialog, but preserve them during tool execution
	filterToolGen := NewBlockWhitelistFilter(toolGen, []string{gai.Content, gai.ToolCall})

	// Create MCP configuration from unified config
	mcpConfig := &mcp.Config{
		MCPServers: mcpServers,
	}

	// Validate MCP configuration if servers exist
	if len(mcpServers) > 0 {
		if err := mcpConfig.Validate(); err != nil {
			return nil, fmt.Errorf("invalid MCP configuration: %w", err)
		}
	}

	// Create client manager
	client := mcpinternal.NewClient()

	// Register MCP server tools
	if err = mcp.RegisterMCPServerTools(ctx, client, *mcpConfig, filterToolGen); err != nil {
		return nil, fmt.Errorf("failed to register MCP tools: %v\n", err)
	}

	return filterToolGen, nil
}
