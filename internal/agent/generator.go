package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	a "github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v2"
	oaiopt "github.com/openai/openai-go/v2/option"
	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/cenkalti/backoff/v5"
)

// prependClaudeCodeIdentifier adds the required Claude Code identifier as the first
// system message. Anthropic requires this for OAuth tokens to work.
func prependClaudeCodeIdentifier(_ context.Context, params *a.MessageNewParams) error {
	claudeCodeIdentifier := a.TextBlockParam{
		Text: "You are Claude Code, Anthropic's official CLI for Claude.",
		CacheControl: a.CacheControlEphemeralParam{
			Type: "ephemeral",
		},
	}
	params.System = append([]a.TextBlockParam{claudeCodeIdentifier}, params.System...)
	return nil
}

func initGeneratorFromModel(
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
		httpClient = &http.Client{Transport: transport}
	}

	apiEnv := strings.TrimSpace(m.ApiKeyEnv)
	apiKey := os.Getenv(apiEnv)

	var gen gai.ToolCapableGenerator

	switch t {
	case "openai":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout))
		oaiGen := gai.NewOpenAiGenerator(&client.Chat.Completions, m.ID, systemPrompt)
		gen = &oaiGen
	case "anthropic":
		var client a.Client
		authMethod := strings.ToLower(m.AuthMethod)

		if authMethod == "oauth" {
			// Use OAuth authentication
			store, err := auth.NewStore()
			if err != nil {
				return nil, fmt.Errorf("initializing auth store: %w", err)
			}
			cred, err := store.GetCredential("anthropic")
			if err != nil {
				return nil, fmt.Errorf("no OAuth credential found for anthropic (run 'cpe auth login anthropic'): %w", err)
			}
			if cred.Type != "oauth" {
				return nil, fmt.Errorf("stored credential is not OAuth type")
			}
			oauthClient := auth.NewOAuthHTTPClient(store)
			// Combine OAuth beta header with interleaved thinking and context management headers
			betaHeaders := auth.AnthropicBetaHeader + ",interleaved-thinking-2025-05-14,context-management-2025-06-27"
			client = a.NewClient(
				aopts.WithBaseURL(baseURL),
				aopts.WithAPIKey("placeholder"),
				aopts.WithHTTPClient(oauthClient),
				aopts.WithRequestTimeout(timeout),
				aopts.WithHeader("anthropic-beta", betaHeaders),
			)
		} else {
			// Use API key authentication
			if apiKey == "" {
				return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
			}
			// Add beta headers for interleaved thinking and context management
			client = a.NewClient(
				aopts.WithBaseURL(baseURL),
				aopts.WithAPIKey(apiKey),
				aopts.WithHTTPClient(httpClient),
				aopts.WithRequestTimeout(timeout),
				aopts.WithHeader("anthropic-beta", "interleaved-thinking-2025-05-14,context-management-2025-06-27"),
			)
		}
		// Build modifier list - always include caching
		modifiers := []gai.AnthropicServiceParamModifierFunc{
			gai.EnableSystemCaching,
			gai.EnableMultiTurnCaching,
		}
		// For OAuth, prepend Claude Code identifier (required by Anthropic for OAuth tokens)
		if authMethod == "oauth" {
			modifiers = append([]gai.AnthropicServiceParamModifierFunc{prependClaudeCodeIdentifier}, modifiers...)
		}
		svc := gai.NewAnthropicServiceWrapper(&client.Messages, modifiers...)
		gen = gai.NewAnthropicGenerator(svc, m.ID, systemPrompt)
	case "gemini":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
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
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		gen = gai.NewCerebrasGenerator(httpClient, baseURL, m.ID, systemPrompt, apiKey)
	case "responses":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout))
		gen = gai.NewResponsesToolGeneratorAdapter(gai.NewResponsesGenerator(&client.Responses, m.ID, systemPrompt), "")
	case "openrouter":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout))
		gen = gai.NewOpenRouterGenerator(&client.Chat.Completions, m.ID, systemPrompt)
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}

	return NewPanicCatchingGenerator(gen), nil
}

// Iface interface for generators that work with gai.Dialog
type Iface interface {
	Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
}

// CreateToolCapableGenerator creates a Iface with all middleware properly configured
func CreateToolCapableGenerator(
	ctx context.Context,
	selectedModel config.Model,
	systemPrompt string,
	requestTimeout time.Duration,
	baseURLOverride string,
	disableStreaming bool,
	mcpServers map[string]mcp.ServerConfig,
	codeModeConfig *config.CodeModeConfig,
) (Iface, error) {
	// Create the base generator from catalog model
	genBase, err := initGeneratorFromModel(ctx, selectedModel, systemPrompt, requestTimeout, baseURLOverride)
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

	// Wrap the tool generator with a filter to handle thinking blocks.
	// For Anthropic: keep Anthropic thinking blocks but filter out thinking blocks
	// from other providers (Gemini, OpenRouter, etc.)
	// For other providers: filter all thinking blocks
	var filterToolGen Iface
	if strings.ToLower(selectedModel.Type) == "anthropic" {
		filterToolGen = NewAnthropicThinkingBlockFilter(toolGen)
	} else {
		filterToolGen = NewBlockWhitelistFilter(toolGen, []string{gai.Content, gai.ToolCall})
	}

	// Wrap with tool result printer to print tool execution results to stderr
	// Use the same content renderer for consistent styling
	toolResultRenderer, err := newContentRenderer()
	if err != nil {
		return nil, fmt.Errorf("failed to create tool result renderer: %w", err)
	}
	toolResultPrinter := NewToolResultPrinterWrapper(filterToolGen, toolResultRenderer)

	// Create client manager
	client := mcp.NewClient()

	// Fetch MCP server tools
	toolsByServer, err := mcp.FetchTools(ctx, client, mcpServers)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MCP tools: %w", err)
	}

	// Check if code mode is enabled
	codeModeEnabled := codeModeConfig != nil && codeModeConfig.Enabled

	if codeModeEnabled {
		// Partition tools into code-mode and excluded
		var excludedToolNames []string
		if codeModeConfig.ExcludedTools != nil {
			excludedToolNames = codeModeConfig.ExcludedTools
		}

		serverToolsInfo, excludedTools := codemode.PartitionTools(toolsByServer, mcpServers, excludedToolNames)

		// Run collision detection on code-mode tools
		codeModeToolNames := codemode.GetCodeModeToolNames(serverToolsInfo)
		if err := codemode.CheckToolNameCollisions(codeModeToolNames); err != nil {
			return nil, err
		}

		// Collect all code-mode tools for tool description generation
		var allCodeModeTools []*mcpsdk.Tool
		for _, serverInfo := range serverToolsInfo {
			allCodeModeTools = append(allCodeModeTools, serverInfo.Tools...)
		}

		// Always register execute_go_code when code mode is enabled, even without MCP tools.
		// The tool provides access to the Go standard library for file I/O, etc.
		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool(allCodeModeTools, codeModeConfig.MaxTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to generate execute_go_code tool: %w", err)
		}

		callback := &codemode.ExecuteGoCodeCallback{
			Servers: serverToolsInfo,
		}

		if err := toolResultPrinter.Register(executeGoCodeTool, callback); err != nil {
			return nil, fmt.Errorf("failed to register execute_go_code tool: %w", err)
		}

		// Register excluded tools normally
		for _, toolData := range excludedTools {
			if err := toolResultPrinter.Register(toolData.Tool, toolData.ToolCallback); err != nil {
				return nil, fmt.Errorf("failed to register excluded tool %s: %w", toolData.Tool.Name, err)
			}
		}
	} else {
		// Code mode disabled: register all tools normally
		for _, tools := range toolsByServer {
			for _, toolData := range tools {
				if err := toolResultPrinter.Register(toolData.Tool, toolData.ToolCallback); err != nil {
					return nil, fmt.Errorf("failed to register tool %s: %w", toolData.Tool.Name, err)
				}
			}
		}
	}

	return toolResultPrinter, nil
}
