package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/spachava753/gai"
	"google.golang.org/genai"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/models"
)

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

// NewProviders binds the credential store and HTTP client used for model execution.
// Call Close when the ACP connection ends to stop credential refresh work.
func NewProviders(store *auth.Store, path string, client *http.Client) (*Providers, error) {
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

// Generator creates a GAI provider adapter and turn-specific generation options.
// Subscription tokens are only attached to their exact HTTPS provider host.
func (p *Providers) Generator(ctx context.Context, model models.Model, effort ReasoningLevel) (gai.Generator, gai.GenerationOptions, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	key := ""
	if model.APIKey != nil {
		key = model.APIKey.ExposeSecret()
	}
	codex := model.API == models.Responses && model.BaseUrl.Scheme == httpsScheme && model.BaseUrl.Host == "chatgpt.com"
	client := p.client
	if key == "" && (codex || model.BaseUrl.Scheme == httpsScheme && model.BaseUrl.Host == "opencode.ai") {
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
			p.client,
			base,
			key,
		)
	case models.Zai:
		generator, err = gai.NewZaiGenerator(
			p.client,
			base,
			key,
		)
	case models.Moonshot:
		generator, err = gai.NewMoonshotGenerator(
			p.client,
			base,
			key,
		)
	case models.Deepseek:
		generator, err = gai.NewDeepSeekGenerator(
			p.client,
			base,
			key,
		)
	case models.Cerebras:
		generator, err = gai.NewCerebrasGenerator(
			p.client,
			base,
			key,
		)
	case models.OpenRouter:
		generator, err = gai.NewOpenRouterGenerator(
			p.client,
			base,
			key,
		)
	case models.Responses:
		opts := []openaioption.RequestOption{
			openaioption.WithHTTPClient(client),
			openaioption.WithBaseURL(base),
			openaioption.WithAPIKey(key),
			openaioption.WithMaxRetries(0),
		}
		client := openai.NewClient(opts...)
		generator = gai.NewResponsesGenerator(&client.Responses)
	case models.Anthropic:
		client := anthropic.NewClient(
			anthropicoption.WithoutEnvironmentDefaults(),
			anthropicoption.WithHTTPClient(p.client),
			anthropicoption.WithBaseURL(base),
			anthropicoption.WithAPIKey(key),
			anthropicoption.WithMaxRetries(0),
		)
		generator = gai.NewAnthropicGenerator(&client.Messages)
	case models.Gemini:
		if key == "" {
			return nil, nil, errors.New("gemini requires an explicit API key")
		}
		var client *genai.Client
		client, err = genai.NewClient(
			ctx,
			&genai.ClientConfig{
				APIKey:     key,
				Backend:    genai.BackendGeminiAPI,
				HTTPClient: p.client,
				HTTPOptions: genai.HTTPOptions{
					BaseURL: base,
				},
			},
		)
		if err == nil {
			generator = gai.NewGeminiGenerator(client)
		}
	default:
		err = fmt.Errorf(
			"unsupported model API %v",
			model.API,
		)
	}
	return generator, options, err
}
