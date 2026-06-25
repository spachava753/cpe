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
	"github.com/anthropics/anthropic-sdk-go/vertex"
	"github.com/openai/openai-go/v3"
	oaiopt "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/spachava753/gai"
	"golang.org/x/oauth2/google"
	gapioption "google.golang.org/api/option"
	gapitransport "google.golang.org/api/transport"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/config"
	"github.com/spachava753/cpe/internal/httpclient"
)

const (
	authMethodOAuth            = "oauth"
	modelTypeAnthropicVertex   = "anthropic_vertex"
	googleCloudPlatformScope   = "https://www.googleapis.com/auth/cloud-platform"
	anthropicFeatureBetaHeader = "interleaved-thinking-2025-05-14,context-management-2025-06-27"
)

var findDefaultGoogleCredentials = google.FindDefaultCredentials

// ModelTypeResponses is the model type identifier for the OpenAI Responses API.
const ModelTypeResponses = "responses"

func newModelHTTPClient(timeout time.Duration) *http.Client {
	return httpclient.New(modelHTTPClientOptions(timeout)...)
}

func newModelRoundTripper(base http.RoundTripper) http.RoundTripper {
	opts := modelHTTPClientOptions(0)
	opts = append(opts, httpclient.WithBaseTransport(base))
	return httpclient.Transport(opts...)
}

func modelHTTPClientOptions(timeout time.Duration) []httpclient.Option {
	opts := []httpclient.Option{
		httpclient.WithBackoff(500*time.Millisecond, 30*time.Second),
		httpclient.WithJitterFactor(0.2),
		httpclient.WithMaxRetries(3),
	}
	if timeout > 0 {
		opts = append(opts, httpclient.WithTimeout(timeout))
	}
	return opts
}

func openAIRequestOptions(apiKey string, httpClient *http.Client, timeout time.Duration) []oaiopt.RequestOption {
	return []oaiopt.RequestOption{
		oaiopt.WithAPIKey(apiKey),
		oaiopt.WithHTTPClient(httpClient),
		oaiopt.WithRequestTimeout(timeout),
		oaiopt.WithMaxRetries(0),
	}
}

func anthropicRequestOptions(apiKey string, httpClient *http.Client, timeout time.Duration) []aopts.RequestOption {
	return []aopts.RequestOption{
		aopts.WithAPIKey(apiKey),
		aopts.WithHTTPClient(httpClient),
		aopts.WithRequestTimeout(timeout),
		aopts.WithMaxRetries(0),
	}
}

func anthropicVertexRequestOptions(
	ctx context.Context,
	vertexCfg *config.VertexConfig,
	patchConfig *config.PatchRequestConfig,
	timeout time.Duration,
) (opts []aopts.RequestOption, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("configuring Google Vertex AI client: %v", recovered)
		}
	}()

	if vertexCfg == nil {
		return nil, fmt.Errorf("vertex configuration is required for %s models", modelTypeAnthropicVertex)
	}
	projectID := strings.TrimSpace(vertexCfg.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("vertex.project_id is required for %s models", modelTypeAnthropicVertex)
	}
	region := strings.TrimSpace(vertexCfg.Region)
	if region == "" {
		return nil, fmt.Errorf("vertex.region is required for %s models", modelTypeAnthropicVertex)
	}

	creds, err := findDefaultGoogleCredentials(ctx, vertexScopes(vertexCfg)...)
	if err != nil {
		return nil, fmt.Errorf("finding Google Application Default Credentials for Vertex AI: %w", err)
	}

	googleHTTPClient, _, err := gapitransport.NewHTTPClient(ctx, gapioption.WithTokenSource(creds.TokenSource))
	if err != nil {
		return nil, fmt.Errorf("creating Google Vertex AI HTTP client: %w", err)
	}
	transport := googleHTTPClient.Transport
	if patchConfig != nil {
		// Vertex middleware rewrites the Anthropic request before the HTTP client runs;
		// keep Google's auth transport underneath PatchTransport so IAM auth wins.
		transport, err = BuildPatchTransportFromConfig(transport, patchConfig)
		if err != nil {
			return nil, fmt.Errorf("building patch transport for Vertex AI: %w", err)
		}
	}
	googleHTTPClient.Transport = newModelRoundTripper(transport)
	googleHTTPClient.Timeout = timeout

	opts = []aopts.RequestOption{
		aopts.WithoutEnvironmentDefaults(),
		aopts.WithHeader("anthropic-beta", anthropicFeatureBetaHeader),
		vertex.WithCredentials(ctx, region, projectID, creds),
		aopts.WithHTTPClient(googleHTTPClient),
		aopts.WithRequestTimeout(timeout),
		aopts.WithMaxRetries(0),
	}
	return opts, nil
}

func vertexScopes(vertexCfg *config.VertexConfig) []string {
	if len(vertexCfg.Scopes) > 0 {
		return vertexCfg.Scopes
	}
	return []string{googleCloudPlatformScope}
}

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

func InitGeneratorFromModel(
	ctx context.Context,
	m config.Model,
	systemPrompt string,
	timeout time.Duration,
) (gai.Generator, error) {
	t := strings.ToLower(m.Type)
	baseURL := m.BaseUrl

	httpClient := newModelHTTPClient(timeout)
	if m.PatchRequest != nil {
		transport, err := BuildPatchTransportFromConfig(httpClient.Transport, m.PatchRequest)
		if err != nil {
			return nil, fmt.Errorf("building patch transport: %w", err)
		}
		httpClient = &http.Client{Transport: transport, Timeout: timeout}
	}

	apiEnv := strings.TrimSpace(m.ApiKeyEnv)
	apiKey := os.Getenv(apiEnv)

	var gen gai.ToolCallingGenerator

	switch t {
	case "openai":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		oaiOpts := openAIRequestOptions(apiKey, httpClient, timeout)
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
				return nil, fmt.Errorf("no OAuth credential found for anthropic (run 'cpe account login anthropic'): %w", err)
			}
			if cred.Type != "oauth" {
				return nil, fmt.Errorf("stored credential is not OAuth type")
			}
			// Build transport chain: PatchTransport -> OAuthTransport -> failsafe -> DefaultTransport.
			// This order ensures OAuthTransport merges its headers with any headers
			// set by PatchTransport, rather than PatchTransport overwriting OAuth headers.
			oauthTransport := auth.NewOAuthTransport(newModelRoundTripper(nil), store)
			var finalTransport http.RoundTripper = oauthTransport
			if m.PatchRequest != nil {
				// Wrap OAuthTransport with PatchTransport
				finalTransport, err = BuildPatchTransportFromConfig(oauthTransport, m.PatchRequest)
				if err != nil {
					return nil, fmt.Errorf("building patch transport for OAuth: %w", err)
				}
			}
			oauthClient := &http.Client{Transport: finalTransport, Timeout: timeout}
			anthOpts := anthropicRequestOptions("placeholder", oauthClient, timeout)
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
			anthOpts := append(anthropicRequestOptions(apiKey, httpClient, timeout),
				aopts.WithHeader("anthropic-beta", anthropicFeatureBetaHeader),
			)
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
	case modelTypeAnthropicVertex:
		anthOpts, err := anthropicVertexRequestOptions(ctx, m.Vertex, m.PatchRequest, timeout)
		if err != nil {
			return nil, err
		}
		client := a.NewClient(anthOpts...)
		modifiers := []gai.AnthropicServiceParamModifierFunc{
			gai.EnableSystemCaching,
			gai.EnableMultiTurnCaching,
		}
		svc := gai.NewAnthropicServiceWrapper(&client.Messages, modifiers...)
		gen = gai.NewAnthropicGenerator(svc, m.ID, systemPrompt)
	case "gemini":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		httpOptions := genai.HTTPOptions{BaseURL: baseURL}
		if timeout > 0 {
			httpOptions.Timeout = &timeout
		}
		cc := genai.ClientConfig{
			APIKey:      apiKey,
			HTTPClient:  httpClient,
			HTTPOptions: httpOptions,
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
				return nil, fmt.Errorf("no OAuth credential found for openai (run 'cpe account login openai'): %w", err)
			}
			if cred.Type != "oauth" {
				return nil, fmt.Errorf("stored credential is not OAuth type")
			}
			// Build transport chain: PatchTransport -> OpenAIOAuthTransport -> failsafe -> DefaultTransport.
			oauthTransport := auth.NewOpenAIOAuthTransport(newModelRoundTripper(nil), store)
			var finalTransport http.RoundTripper = oauthTransport
			if m.PatchRequest != nil {
				finalTransport, err = BuildPatchTransportFromConfig(oauthTransport, m.PatchRequest)
				if err != nil {
					return nil, fmt.Errorf("building patch transport for OAuth: %w", err)
				}
			}
			oauthClient := &http.Client{Transport: finalTransport, Timeout: timeout}

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

			respOpts := append(openAIRequestOptions("placeholder", oauthClient, timeout),
				oaiopt.WithBaseURL(oauthBaseURL),
			)
			client := openai.NewClient(respOpts...)
			respGen := gai.NewResponsesGenerator(&client.Responses, m.ID, oauthSystemPrompt)
			gen = &respGen
		} else {
			// Use API key authentication
			if apiKey == "" {
				return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
			}
			respOpts := openAIRequestOptions(apiKey, httpClient, timeout)
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
		orOpts := openAIRequestOptions(apiKey, httpClient, timeout)
		if baseURL != "" {
			orOpts = append(orOpts, oaiopt.WithBaseURL(baseURL))
		}
		client := openai.NewClient(orOpts...)
		gen = gai.NewOpenRouterGenerator(&client.Chat.Completions, m.ID, systemPrompt)
	case "zai":
		if apiKey == "" {
			return nil, fmt.Errorf("API key missing: %s not set", apiEnv)
		}
		// zaiOpts := openAIRequestOptions(apiKey, httpClient, timeout)
		// if baseURL != "" {
		// 	zaiOpts = append(zaiOpts, oaiopt.WithBaseURL(baseURL))
		// }
		// client := openai.NewClient(zaiOpts...)
		// gen = gai.NewZaiGenerator(&client.Chat.Completions, m.ID, systemPrompt, apiKey)
		panic("zai provider unimplemented")
	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.Type)
	}

	// If the generator supports streaming, wrap it with StreamingAdapter.
	// This uses streaming for the actual API call (avoiding HTTP timeouts on
	// long-running generations) while converting the streamed response back
	// into a standard Response for the rest of the middleware stack.
	if sg, ok := gen.(gai.StreamingGenerator); ok {
		gen = &gai.StreamingAdapter{S: sg}
	}
	if t == ModelTypeResponses {
		gen = newResponsesPhaseRetryGenerator(gen)
	}

	return gen, nil
}
