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
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/types"

	"github.com/cenkalti/backoff/v5"
)

const authMethodOAuth = "oauth"

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
) (gai.Generator, error) {
	t := strings.ToLower(m.Type)
	baseURL := m.BaseUrl

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

		if authMethod == authMethodOAuth {
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
			// Build transport chain: PatchTransport -> OAuthTransport -> DefaultTransport
			// This order ensures OAuthTransport merges its headers with any headers
			// set by PatchTransport, rather than PatchTransport overwriting OAuth headers.
			oauthTransport := auth.NewOAuthTransport(nil, store)
			var finalTransport http.RoundTripper = oauthTransport
			if m.PatchRequest != nil {
				// Wrap OAuthTransport with PatchTransport
				finalTransport, err = BuildPatchTransportFromConfig(oauthTransport, m.PatchRequest)
				if err != nil {
					return nil, fmt.Errorf("building patch transport for OAuth: %w", err)
				}
			}
			oauthClient := &http.Client{Transport: finalTransport, Timeout: 5 * time.Minute}
			client = a.NewClient(
				aopts.WithBaseURL(baseURL),
				aopts.WithAPIKey("placeholder"),
				aopts.WithHTTPClient(oauthClient),
				aopts.WithRequestTimeout(timeout),
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
		if authMethod == authMethodOAuth {
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
	case "zai":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		client := openai.NewClient(oaiopt.WithBaseURL(baseURL), oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout))
		gen = gai.NewZaiGenerator(&client.Chat.Completions, m.ID, systemPrompt, apiKey)
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}

	return NewPanicCatchingGenerator(gen), nil
}

// ToolCallbackWrapper is a function that wraps a tool callback.
// It receives the tool name and the original callback, and returns a wrapped callback.
// This is used for adding behavior like event emission to tool callbacks.
type ToolCallbackWrapper func(toolName string, callback gai.ToolCallback) gai.ToolCallback

// CreateToolCapableGenerator creates a types.Generator with all middleware properly configured.
// The subagentLoggingAddress parameter specifies the HTTP endpoint where subagent events
// should be sent. If non-empty, it will be injected into child MCP server processes
// via the CPE_SUBAGENT_LOGGING_ADDRESS environment variable.
func CreateToolCapableGenerator(
	ctx context.Context,
	selectedModel config.Model,
	systemPrompt string,
	requestTimeout time.Duration,
	disablePrinting bool,
	mcpServers map[string]mcp.ServerConfig,
	codeModeConfig *config.CodeModeConfig,
	subagentLoggingAddress string,
	callbackWrapper ToolCallbackWrapper,
) (types.Generator, error) {
	// Create the base generator from catalog model
	genBase, err := initGeneratorFromModel(ctx, selectedModel, systemPrompt, requestTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create generator: %w", err)
	}

	// Cast to ToolCapableGenerator
	gen, ok := genBase.(gai.ToolCapableGenerator)
	if !ok {
		return nil, fmt.Errorf("generator does not implement ToolCapableGenerator interface")
	}

	// Build middleware stack using gai.Wrap
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 1 * time.Minute
	b.Reset()

	var wrappers []gai.WrapperFunc
	// TokenUsagePrinting must come BEFORE ResponsePrinting in the slice
	// because gai.Wrap applies wrappers in reverse order.
	// We want TokenUsagePrinting to be OUTERMOST so it prints AFTER response content.
	wrappers = append(wrappers, WithTokenUsagePrinting(os.Stderr))
	if !disablePrinting {
		renderers := NewResponsePrinterRenderers()
		wrappers = append(wrappers, WithResponsePrinting(renderers.Content, renderers.Thinking, renderers.ToolCall, os.Stdout, os.Stderr))
	}

	// Add block filter based on model type:
	// For Anthropic: keep Anthropic thinking blocks but filter out thinking blocks from other providers
	// For other providers: filter all thinking blocks, keep only content and tool calls
	if strings.ToLower(selectedModel.Type) == "anthropic" {
		wrappers = append(wrappers, WithAnthropicThinkingFilter())
	} else {
		wrappers = append(wrappers, WithBlockWhitelist([]string{gai.Content, gai.ToolCall}))
	}

	wrappers = append(wrappers, gai.WithRetry(b, backoff.WithMaxTries(3), backoff.WithMaxElapsedTime(5*time.Minute)))

	toolResultRenderer := NewRenderer()
	wrappers = append(wrappers, WithToolResultPrinterWrapper(toolResultRenderer))

	gen = gai.Wrap(gen, wrappers...).(gai.ToolCapableGenerator)

	// Create the tool generator using the wrapped generator
	toolGen := &gai.ToolGenerator{
		G: gen,
	}

	// Create client manager
	client := mcp.NewClient()

	// Fetch MCP server tools
	toolsByServer, err := mcp.FetchTools(ctx, client, mcpServers, subagentLoggingAddress)
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

		finalCallback := gai.ToolCallback(callback)
		if callbackWrapper != nil {
			finalCallback = callbackWrapper(executeGoCodeTool.Name, callback)
		}
		if err := toolGen.Register(executeGoCodeTool, finalCallback); err != nil {
			return nil, fmt.Errorf("failed to register execute_go_code tool: %w", err)
		}

		// Register excluded tools normally
		for _, toolData := range excludedTools {
			cb := toolData.ToolCallback
			if callbackWrapper != nil {
				cb = callbackWrapper(toolData.Tool.Name, toolData.ToolCallback)
			}
			if err := toolGen.Register(toolData.Tool, cb); err != nil {
				return nil, fmt.Errorf("failed to register excluded tool %s: %w", toolData.Tool.Name, err)
			}
		}
	} else {
		// Code mode disabled: register all tools normally
		for _, tools := range toolsByServer {
			for _, toolData := range tools {
				cb := toolData.ToolCallback
				if callbackWrapper != nil {
					cb = callbackWrapper(toolData.Tool.Name, toolData.ToolCallback)
				}
				if err := toolGen.Register(toolData.Tool, cb); err != nil {
					return nil, fmt.Errorf("failed to register tool %s: %w", toolData.Tool.Name, err)
				}
			}
		}
	}

	return toolGen, nil
}
