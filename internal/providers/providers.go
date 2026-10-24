package providers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/conversation"
	"github.com/spachava753/cpe/internal/models"
)

const httpsScheme = "https"

// Providers resolves configured API models and locally stored subscription credentials.
// ChatGPT credentials are refreshed near expiry and reread for every HTTP request.
type Providers struct {
	store   *auth.Store
	path    string
	client  *http.Client
	mu      sync.Mutex
	closed  bool
	refresh *credentialRefresh
	workers sync.WaitGroup
}

// New binds the credential store and HTTP client used for model execution.
// Call Close when the ACP connection ends to stop credential refresh work.
func New(store *auth.Store, path string, client *http.Client) (*Providers, error) {
	if store == nil || path == "" || client == nil {
		return nil, errors.New("credential store, path, and HTTP client are required")
	}
	return &Providers{
		store:  store,
		path:   path,
		client: client,
	}, nil
}

// Authenticate verifies that terminal login established usable local credentials.
func (p *Providers) Authenticate(ctx context.Context, provider auth.Provider) error {
	if provider == auth.OpenAI {
		_, err := p.openAICredentials(ctx)
		return err
	}
	credentials, err := p.store.Retrieve(
		ctx,
		p.path,
	)
	if err != nil {
		return err
	}
	switch provider {
	case auth.OpenCode:
		if credentials.OpenCode == nil {
			return errors.New("run cpe auth login opencode before continuing")
		}
	default:
		return errors.New("unknown authentication provider")
	}
	return nil
}

// Logout removes both local subscription entries without revoking remote sessions.
func (p *Providers) Logout(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopRefresh()
	return errors.Join(
		p.store.Save(
			ctx,
			p.path,
			auth.OpenAI,
			auth.Credentials{},
		),
		p.store.Save(
			ctx,
			p.path,
			auth.OpenCode,
			auth.Credentials{},
		),
	)
}

// Generator creates a provider adapter and options for the conversation.
// Subscription tokens and OpenCode session headers are restricted to their exact HTTPS hosts.
func (p *Providers) Generator(ctx context.Context, sessionID conversation.ID, model models.Model, effort models.ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	key := ""
	if model.APIKey != nil {
		key = model.APIKey.ExposeSecret()
	}
	codex := model.API == models.Responses && model.BaseUrl.Scheme == httpsScheme && model.BaseUrl.Host == "chatgpt.com"
	opencode := model.BaseUrl.Scheme == httpsScheme && model.BaseUrl.Host == "opencode.ai"
	if opencode && sessionID == "" {
		return nil, nil, errors.New("OpenCode requires a conversation ID")
	}
	client := p.client
	if key == "" && (codex || opencode) {
		credentials, err := p.store.Retrieve(
			ctx,
			p.path,
		)
		if err != nil {
			return nil, nil, err
		}
		if codex {
			if _, err := p.openAICredentials(ctx); err != nil {
				return nil, nil, err
			}
			copy := *p.client
			transport := p.client.Transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			copy.Transport = subscriptionTransport{
				providers: p,
				base:      transport,
			}
			copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			client = &copy
		} else {
			if credentials.OpenCode == nil {
				return nil, nil, errors.New("OpenCode Go login missing; run cpe auth login opencode")
			}
			key = credentials.OpenCode.APIKey
		}
	}
	if opencode {
		copy := *p.client
		transport := p.client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		copy.Transport = openCodeTransport{
			base:      transport,
			sessionID: sessionID,
		}
		client = &copy
	}
	options := gai.NewGenerationOptions(gai.WithMaxGenerationTokens(int(model.MaxTokens)))
	if codex {
		options = gai.GenerationOptions{}
	}
	if effort != "" {
		gai.WithReasoningEffort(string(effort))(options)
	}
	if model.UseMaxTokens {
		gai.WithOpenAITokenLimitField("max_tokens")(options)
	}
	if len(model.ExtraFields) != 0 {
		gai.WithOpenAIExtraBody(model.ExtraFields)(options)
	}
	base := model.BaseUrl.String()
	var generator gai.Generator
	var err error
	switch model.API {
	case models.Openai:
		generator, err = gai.NewOpenAiGenerator(
			client,
			base,
			key,
		)
	case models.Zai:
		generator, err = gai.NewZaiGenerator(
			client,
			base,
			key,
		)
	case models.Moonshot:
		generator, err = gai.NewMoonshotGenerator(
			client,
			base,
			key,
		)
	case models.Deepseek:
		generator, err = gai.NewDeepSeekGenerator(
			client,
			base,
			key,
		)
	case models.Cerebras:
		generator, err = gai.NewCerebrasGenerator(
			client,
			base,
			key,
		)
	case models.OpenRouter:
		generator, err = gai.NewOpenRouterGenerator(
			client,
			base,
			key,
		)
	case models.Responses:
		gai.WithResponsesThoughtSummaryDetail("auto")(options)
		opts := []openaioption.RequestOption{
			openaioption.WithHTTPClient(client),
			openaioption.WithBaseURL(base),
			openaioption.WithAPIKey(key),
			openaioption.WithMaxRetries(0),
		}
		client := openai.NewClient(opts...)
		generator = gai.NewResponsesGenerator(&client.Responses)
	case models.Anthropic:
		if effort == "" {
			gai.WithReasoningEffort("adaptive")(options)
		}
		client := anthropic.NewClient(
			anthropicoption.WithoutEnvironmentDefaults(),
			anthropicoption.WithHTTPClient(client),
			anthropicoption.WithBaseURL(base),
			anthropicoption.WithAPIKey(key),
			anthropicoption.WithMaxRetries(0),
		)
		generator = gai.NewAnthropicGenerator(gai.NewAnthropicServiceWrapper(
			&client.Messages,
			summarizeAnthropicThinking,
			stabilizeAnthropicToolResults,
			gai.EnableSystemCaching,
			gai.EnableMultiTurnCaching,
		))
	case models.Gemini:
		if key == "" {
			return nil, nil, errors.New("gemini requires an explicit API key")
		}
		var gemini *genai.Client
		gemini, err = genai.NewClient(
			ctx,
			&genai.ClientConfig{
				APIKey:     key,
				Backend:    genai.BackendGeminiAPI,
				HTTPClient: client,
				HTTPOptions: genai.HTTPOptions{
					BaseURL: base,
				},
			},
		)
		if err == nil {
			generator = gai.NewGeminiGenerator(gemini)
		}
	default:
		err = fmt.Errorf(
			"unsupported model API %v",
			model.API,
		)
	}
	return generator, options, err
}

func summarizeAnthropicThinking(_ context.Context, params *anthropic.MessageNewParams) error {
	if params.Thinking.OfAdaptive != nil {
		params.Thinking.OfAdaptive.Display = anthropic.ThinkingConfigAdaptiveDisplaySummarized
	}
	return nil
}

// GAI groups tool results through a map; keep serialized cache prefixes stable.
func stabilizeAnthropicToolResults(_ context.Context, params *anthropic.MessageNewParams) error {
	for _, message := range params.Messages {
		for start := 0; start < len(message.Content); {
			if message.Content[start].OfToolResult == nil {
				start++
				continue
			}
			end := start + 1
			for end < len(message.Content) && message.Content[end].OfToolResult != nil {
				end++
			}
			slices.SortFunc(
				message.Content[start:end],
				func(a, b anthropic.ContentBlockParamUnion) int {
					return cmp.Compare(
						a.OfToolResult.ToolUseID,
						b.OfToolResult.ToolUseID,
					)
				},
			)
			start = end
		}
	}
	return nil
}
