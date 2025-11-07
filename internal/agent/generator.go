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

func InitGeneratorFromModel(
	ctx context.Context,
	m config.Model,
	systemPrompt string,
	timeout time.Duration,
	overrideBaseURL string,
) (gai.Generator, error) {
	t := strings.ToLower(m.Type)
	baseURL := m.BaseUrl
	if overrideBaseURL != "" {
		baseURL = overrideBaseURL
	}

	httpClient := http.DefaultClient
	if m.PatchRequest != nil {
		transport, err := BuildPatchTransportFromConfig(nil, m.PatchRequest)
		if err != nil {
			return nil, fmt.Errorf("building patch transport: %w", err)
		}
		httpClient = &http.Client{Transport: transport, Timeout: timeout}
	}

	apiEnv := strings.TrimSpace(m.ApiKeyEnv)
	apiKey := os.Getenv(apiEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
	}

	var gen gai.ToolCapableGenerator

	switch t {
	case "openai":
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient))
		oaiGen := gai.NewOpenAiGenerator(&client.Chat.Completions, m.ID, systemPrompt)
		gen = &oaiGen
	case "anthropic":
		client := anthropic.NewClient(aopts.WithBaseURL(baseURL), aopts.WithAPIKey(apiKey), aopts.WithHTTPClient(httpClient))
		svc := gai.NewAnthropicServiceWrapper(&client.Messages, gai.EnableSystemCaching, gai.EnableMultiTurnCaching)
		gen = gai.NewAnthropicGenerator(svc, m.ID, systemPrompt)
	case "gemini":
		cc := genai.ClientConfig{
			APIKey:      apiKey,
			HTTPOptions: genai.HTTPOptions{BaseURL: baseURL},
		}
		client, err := genai.NewClient(ctx, &cc)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini client: %w", err)
		}
		gen, err = gai.NewGeminiGenerator(client, m.ID, systemPrompt)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini generator: %w", err)
		}
	case "cerebras":
		gen = gai.NewCerebrasGenerator(httpClient, baseURL, m.ID, systemPrompt, apiKey)
	case "responses":
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient))
		gen = gai.NewResponsesToolGeneratorAdapter(gai.NewResponsesGenerator(&client.Responses, m.ID, systemPrompt), "")
	case "openrouter":
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient))
		gen = gai.NewOpenRouterGenerator(&client.Chat.Completions, m.ID, systemPrompt)
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}

	return NewPanicCatchingGenerator(gen), nil
}

// AgentGenerator interface for generators that work with gai.Dialog
type AgentGenerator interface {
	Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
}

// CreateToolCapableGenerator creates a AgentGenerator with all middleware properly configured
func CreateToolCapableGenerator(
	ctx context.Context,
	selectedModel config.Model,
	systemPrompt string,
	requestTimeout time.Duration,
	baseURLOverride string,
	disableStreaming bool,
	mcpServers map[string]mcp.ServerConfig,
) (AgentGenerator, error) {
	// Create the base generator from catalog model
	genBase, err := InitGeneratorFromModel(ctx, selectedModel, systemPrompt, requestTimeout, baseURLOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to create generator: %w", err)
	}

	// Check if the generator supports streaming and if streaming is enabled
	var gen gai.ToolCapableGenerator
	if streamingGen, ok := genBase.(gai.StreamingGenerator); ok && !disableStreaming {
		streamingPrinter := NewStreamingPrinterGenerator(streamingGen)
		gen = &gai.StreamingAdapter{S: streamingPrinter}
	} else {
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
