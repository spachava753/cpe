package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	a "github.com/anthropics/anthropic-sdk-go"
	aopts "github.com/anthropics/anthropic-sdk-go/option"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3"
	oaiopt "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/codemode"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/mcp"
	"github.com/spachava753/cpe/internal/storage"
	"github.com/spachava753/cpe/internal/types"

	"github.com/cenkalti/backoff/v5"
)

const authMethodOAuth = "oauth"

// ModelTypeResponses is the model type identifier for the OpenAI Responses API.
const ModelTypeResponses = "responses"

// ApplyResponsesThinkingSummary ensures that when using the OpenAI Responses API
// with a thinking budget, the reasoning summary detail parameter is set to "detailed".
//
// The Responses API requires an explicit `reasoning.summary` parameter to return
// thinking/reasoning blocks in the response. Without it, the model reasons internally
// but does not include reasoning content in the output. This function sets the default
// to "detailed" so thinking blocks are visible, unless the user has explicitly
// configured a different value.
//
// This must be called with the correct openai SDK type (responses.ReasoningSummaryDetailed)
// because the gai library performs a type assertion on the ExtraArgs value.
func ApplyResponsesThinkingSummary(opts *gai.GenOpts) {
	if opts == nil || opts.ThinkingBudget == "" {
		return
	}
	if opts.ExtraArgs == nil {
		opts.ExtraArgs = make(map[string]any)
	}
	if _, exists := opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam]; !exists {
		opts.ExtraArgs[gai.ResponsesThoughtSummaryDetailParam] = responses.ReasoningSummaryDetailed
	}
}

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
		oaiOpts := []oaiopt.RequestOption{oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout)}
		if baseURL != "" {
			oaiOpts = append(oaiOpts, oaiopt.WithBaseURL(baseURL))
		}
		client := openai.NewClient(oaiOpts...)
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
			anthOpts := []aopts.RequestOption{
				aopts.WithAPIKey("placeholder"),
				aopts.WithHTTPClient(oauthClient),
				aopts.WithRequestTimeout(timeout),
			}
			if baseURL != "" {
				anthOpts = append(anthOpts, aopts.WithBaseURL(baseURL))
			}
			client = a.NewClient(anthOpts...)
		} else {
			// Use API key authentication
			if apiKey == "" {
				return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
			}
			// Add beta headers for interleaved thinking and context management
			anthOpts := []aopts.RequestOption{
				aopts.WithAPIKey(apiKey),
				aopts.WithHTTPClient(httpClient),
				aopts.WithRequestTimeout(timeout),
				aopts.WithHeader("anthropic-beta", "interleaved-thinking-2025-05-14,context-management-2025-06-27"),
			}
			if baseURL != "" {
				anthOpts = append(anthOpts, aopts.WithBaseURL(baseURL))
			}
			client = a.NewClient(anthOpts...)
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
		authMethod := strings.ToLower(m.AuthMethod)

		if authMethod == authMethodOAuth {
			// Use OAuth authentication for OpenAI (ChatGPT subscription)
			store, err := auth.NewStore()
			if err != nil {
				return nil, fmt.Errorf("initializing auth store: %w", err)
			}
			cred, err := store.GetCredential("openai")
			if err != nil {
				return nil, fmt.Errorf("no OAuth credential found for openai (run 'cpe auth login openai'): %w", err)
			}
			if cred.Type != "oauth" {
				return nil, fmt.Errorf("stored credential is not OAuth type")
			}
			// Build transport chain: PatchTransport -> OpenAIOAuthTransport -> DefaultTransport
			oauthTransport := auth.NewOpenAIOAuthTransport(nil, store)
			var finalTransport http.RoundTripper = oauthTransport
			if m.PatchRequest != nil {
				finalTransport, err = BuildPatchTransportFromConfig(oauthTransport, m.PatchRequest)
				if err != nil {
					return nil, fmt.Errorf("building patch transport for OAuth: %w", err)
				}
			}
			oauthClient := &http.Client{Transport: finalTransport, Timeout: 5 * time.Minute}

			// For OAuth, use the ChatGPT backend API URL unless explicitly overridden
			oauthBaseURL := auth.OpenAICodexBaseURL
			if baseURL != "" {
				oauthBaseURL = baseURL
			}

			// The ChatGPT backend API requires the "instructions" field to be present
			// in every request. Ensure a non-empty system prompt.
			oauthSystemPrompt := systemPrompt
			if oauthSystemPrompt == "" {
				oauthSystemPrompt = "You are a helpful assistant."
			}

			respOpts := []oaiopt.RequestOption{
				oaiopt.WithAPIKey("placeholder"),
				oaiopt.WithHTTPClient(oauthClient),
				oaiopt.WithRequestTimeout(timeout),
				oaiopt.WithBaseURL(oauthBaseURL),
			}
			client := openai.NewClient(respOpts...)
			respGen := gai.NewResponsesGenerator(&client.Responses, m.ID, oauthSystemPrompt)
			gen = &respGen
		} else {
			// Use API key authentication
			if apiKey == "" {
				return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
			}
			respOpts := []oaiopt.RequestOption{oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout)}
			if baseURL != "" {
				respOpts = append(respOpts, oaiopt.WithBaseURL(baseURL))
			}
			client := openai.NewClient(respOpts...)
			respGen := gai.NewResponsesGenerator(&client.Responses, m.ID, systemPrompt)
			gen = &respGen
		}
	case "openrouter":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		orOpts := []oaiopt.RequestOption{oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout)}
		if baseURL != "" {
			orOpts = append(orOpts, oaiopt.WithBaseURL(baseURL))
		}
		client := openai.NewClient(orOpts...)
		gen = gai.NewOpenRouterGenerator(&client.Chat.Completions, m.ID, systemPrompt)
	case "zai":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		zaiOpts := []oaiopt.RequestOption{oaiopt.WithAPIKey(apiKey), oaiopt.WithHTTPClient(httpClient), oaiopt.WithRequestTimeout(timeout)}
		if baseURL != "" {
			zaiOpts = append(zaiOpts, oaiopt.WithBaseURL(baseURL))
		}
		client := openai.NewClient(zaiOpts...)
		gen = gai.NewZaiGenerator(&client.Chat.Completions, m.ID, systemPrompt, apiKey)
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}

	// If the generator supports streaming, wrap it with StreamingAdapter.
	// This uses streaming for the actual API call (avoiding HTTP timeouts on
	// long-running generations) while converting the streamed response back
	// into a standard Response for the rest of the middleware stack.
	if sg, ok := gen.(gai.StreamingGenerator); ok {
		switch t {
		case ModelTypeResponses:
			// The Responses API stores ResponsesPrevRespId in every block's
			// ExtraFields via its non-streaming Generate, but the streaming
			// path only puts it in the metadata block. Use a wrapper that
			// propagates it from UsageMetadata to block ExtraFields.
			gen = &streamingAdapterWithMetadataPropagate{
				StreamingAdapter: gai.StreamingAdapter{S: sg},
				propagateKeys:    []string{gai.ResponsesPrevRespId},
			}
		default:
			gen = &gai.StreamingAdapter{S: sg}
		}
	}

	return NewPanicCatchingGenerator(gen), nil
}

// ToolCallbackWrapper is a function that wraps a tool callback.
// It receives the tool name and the original callback, and returns a wrapped callback.
// This is used for adding behavior like event emission to tool callbacks.
type ToolCallbackWrapper func(toolName string, callback gai.ToolCallback) gai.ToolCallback

// generatorOptions holds optional configuration for generator creation.
type generatorOptions struct {
	disablePrinting bool
	callbackWrapper ToolCallbackWrapper
	middleware      []gai.WrapperFunc
	baseGenerator   gai.ToolCapableGenerator
	dialogSaver     storage.DialogSaver
	stdout          io.Writer
}

// GeneratorOption is a functional option for configuring generator creation.
type GeneratorOption func(*generatorOptions)

// WithDisablePrinting disables response printing to stdout.
// Use this for non-interactive modes like MCP server mode.
func WithDisablePrinting() GeneratorOption {
	return func(o *generatorOptions) {
		o.disablePrinting = true
	}
}

// WithCallbackWrapper sets a wrapper function for tool callbacks.
// This is used for adding behavior like event emission to tool callbacks.
func WithCallbackWrapper(w ToolCallbackWrapper) GeneratorOption {
	return func(o *generatorOptions) {
		o.callbackWrapper = w
	}
}

// WithMiddleware adds custom middleware to the generator's middleware stack.
// Custom middleware is applied after the default middleware (retry, printing, etc.).
func WithMiddleware(m ...gai.WrapperFunc) GeneratorOption {
	return func(o *generatorOptions) {
		o.middleware = append(o.middleware, m...)
	}
}

// WithBaseGenerator sets a custom base generator instead of using the default
// model-based initialization. This is useful for testing or for injecting
// custom generator implementations.
func WithBaseGenerator(g gai.ToolCapableGenerator) GeneratorOption {
	return func(o *generatorOptions) {
		o.baseGenerator = g
	}
}

// WithDialogSaver enables incremental dialog saving via the SavingMiddleware.
// When provided, messages are saved as they flow through the generation pipeline.
// If not provided (nil), no saving occurs (incognito mode).
func WithDialogSaver(saver storage.DialogSaver) GeneratorOption {
	return func(o *generatorOptions) {
		o.dialogSaver = saver
	}
}

// WithStdout sets the writer for model response output.
// If not provided (nil), defaults to os.Stdout.
func WithStdout(w io.Writer) GeneratorOption {
	return func(o *generatorOptions) {
		o.stdout = w
	}
}

// NewGenerator creates a types.Generator with all middleware properly configured.
// It expects an already-initialized MCPState with connections and filtered tools.
//
// Required parameters:
//   - ctx: Context for initialization
//   - cfg: Configuration containing model, timeout, and code mode settings
//   - systemPrompt: The system prompt for the generator
//   - mcpState: Initialized MCP state with connections and tools
//
// Optional parameters (via functional options):
//   - WithDisablePrinting(): Disable response printing
//   - WithCallbackWrapper(w): Set a tool callback wrapper
//   - WithMiddleware(m...): Add custom middleware
//   - WithBaseGenerator(g): Use a custom base generator instead of model-based initialization
func NewGenerator(
	ctx context.Context,
	cfg *config.Config,
	systemPrompt string,
	mcpState *mcp.MCPState,
	opts ...GeneratorOption,
) (types.Generator, error) {
	// Apply options
	o := &generatorOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// Use custom base generator if provided, otherwise create from model config
	var gen gai.ToolCapableGenerator
	if o.baseGenerator != nil {
		gen = o.baseGenerator
	} else {
		genBase, err := initGeneratorFromModel(ctx, cfg.Model, systemPrompt, cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create generator: %w", err)
		}

		var ok bool
		gen, ok = genBase.(gai.ToolCapableGenerator)
		if !ok {
			return nil, fmt.Errorf("generator does not implement ToolCapableGenerator interface")
		}
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
	stdoutW := o.stdout
	if stdoutW == nil {
		stdoutW = os.Stdout
	}

	wrappers = append(wrappers, WithTokenUsagePrinting(os.Stderr))
	if !o.disablePrinting {
		renderers := NewResponsePrinterRenderers()
		wrappers = append(wrappers, WithResponsePrinting(renderers.Content, renderers.Thinking, renderers.ToolCall, stdoutW, os.Stderr))
	}

	// Add saving middleware if storage is provided.
	// Saving must be:
	// - OUTSIDE ThinkingFilter so that SetMessageID mutates the original dialog
	//   messages from ToolGenerator.currentDialog. If ThinkingFilter were between
	//   ToolGenerator and Saving, it would create new message structs, and the IDs
	//   set by Saving would not propagate back — causing double saves on the next
	//   tool-use loop iteration.
	// - OUTSIDE Retry so that messages are saved once, not on each retry attempt.
	//
	// Wrapper ordering (gai.Wrap applies in order, first = outermost):
	// - TokenUsagePrinting (outermost): prints usage AFTER everything
	// - ResponsePrinting: prints response WITH ID after Saving sets it
	// - Saving: BEFORE saves new messages; AFTER saves response, sets ID
	// - ThinkingFilter: filters thinking blocks for the provider
	// - Retry: retries transient failures
	// - ToolResultPrinting (innermost): prints tool results WITH ID
	if o.dialogSaver != nil {
		wrappers = append(wrappers, WithSaving(o.dialogSaver))
	}

	// Add thinking block filter based on model type.
	// Each provider keeps thinking blocks from its own generator type, filtering out
	// thinking blocks from other providers. This enables cross-model conversation resumption
	// while preserving thinking context when switching back to earlier used models.
	switch strings.ToLower(cfg.Model.Type) {
	case "anthropic":
		wrappers = append(wrappers, WithAnthropicThinkingFilter())
	case "gemini":
		wrappers = append(wrappers, WithGeminiThinkingFilter())
	case "openrouter":
		wrappers = append(wrappers, WithOpenRouterThinkingFilter())
	case "responses":
		wrappers = append(wrappers, WithResponsesThinkingFilter())
	case "cerebras":
		wrappers = append(wrappers, WithCerebrasThinkingFilter())
	case "zai":
		wrappers = append(wrappers, WithZaiThinkingFilter())
	case "openai":
		// OpenAI (non-responses) doesn't produce thinking blocks, filter all
		wrappers = append(wrappers, WithBlockWhitelist([]string{gai.Content, gai.ToolCall}))
	default:
		// For unknown providers, filter all thinking blocks
		wrappers = append(wrappers, WithBlockWhitelist([]string{gai.Content, gai.ToolCall}))
	}

	wrappers = append(wrappers, gai.WithRetry(b, backoff.WithMaxTries(3), backoff.WithMaxElapsedTime(5*time.Minute)))

	toolResultRenderer := NewRenderer()
	wrappers = append(wrappers, WithToolResultPrinterWrapper(toolResultRenderer))

	// Add custom middleware after default middleware
	wrappers = append(wrappers, o.middleware...)

	wrapped := gai.Wrap(gen, wrappers...)
	gen, ok := wrapped.(gai.ToolCapableGenerator)
	if !ok {
		return nil, fmt.Errorf("wrapped generator does not implement ToolCapableGenerator interface")
	}

	// Create the tool generator using the wrapped generator
	toolGen := &gai.ToolGenerator{
		G: gen,
	}

	// Check if code mode is enabled
	codeModeEnabled := cfg.CodeMode != nil && cfg.CodeMode.Enabled

	if codeModeEnabled {
		// Partition tools into code-mode and excluded
		var excludedToolNames []string
		if cfg.CodeMode.ExcludedTools != nil {
			excludedToolNames = cfg.CodeMode.ExcludedTools
		}

		codeModeServers, excludedByServer := codemode.PartitionTools(mcpState, excludedToolNames)

		// Run collision detection on code-mode tools
		codeModeToolNames := codemode.GetCodeModeToolNames(codeModeServers)
		if err := codemode.CheckToolNameCollisions(codeModeToolNames); err != nil {
			return nil, err
		}

		// Collect all code-mode tools for tool description generation
		var allCodeModeTools []*mcpsdk.Tool
		for _, serverInfo := range codeModeServers {
			allCodeModeTools = append(allCodeModeTools, serverInfo.Tools...)
		}

		// Always register execute_go_code when code mode is enabled, even without MCP tools.
		// The tool provides access to the Go standard library for file I/O, etc.
		executeGoCodeTool, err := codemode.GenerateExecuteGoCodeTool(allCodeModeTools, cfg.CodeMode.MaxTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to generate execute_go_code tool: %w", err)
		}

		callback := &codemode.ExecuteGoCodeCallback{
			Servers: codeModeServers,
		}

		finalCallback := gai.ToolCallback(callback)
		if o.callbackWrapper != nil {
			finalCallback = o.callbackWrapper(executeGoCodeTool.Name, callback)
		}
		if err := toolGen.Register(executeGoCodeTool, finalCallback); err != nil {
			return nil, fmt.Errorf("failed to register execute_go_code tool: %w", err)
		}

		// Register excluded tools normally
		for serverName, tools := range excludedByServer {
			conn := mcpState.Connections[serverName]
			for _, mcpTool := range tools {
				gaiTool, err := mcp.ToGaiTool(mcpTool)
				if err != nil {
					return nil, fmt.Errorf("converting tool %s: %w", mcpTool.Name, err)
				}
				cb := gai.ToolCallback(mcp.NewToolCallback(conn.ClientSession, serverName, mcpTool.Name))
				if o.callbackWrapper != nil {
					cb = o.callbackWrapper(mcpTool.Name, cb)
				}
				if err := toolGen.Register(gaiTool, cb); err != nil {
					return nil, fmt.Errorf("failed to register excluded tool %s: %w", mcpTool.Name, err)
				}
			}
		}
	} else {
		// Code mode disabled: register all tools normally
		for serverName, conn := range mcpState.Connections {
			for _, mcpTool := range conn.Tools {
				gaiTool, err := mcp.ToGaiTool(mcpTool)
				if err != nil {
					return nil, fmt.Errorf("converting tool %s: %w", mcpTool.Name, err)
				}
				cb := gai.ToolCallback(mcp.NewToolCallback(conn.ClientSession, serverName, mcpTool.Name))
				if o.callbackWrapper != nil {
					cb = o.callbackWrapper(mcpTool.Name, cb)
				}
				if err := toolGen.Register(gaiTool, cb); err != nil {
					return nil, fmt.Errorf("failed to register tool %s: %w", mcpTool.Name, err)
				}
			}
		}
	}

	return toolGen, nil
}
