package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/auth"
)

const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// SupportedAccountProviders is the list of account providers supported by CPE.
var SupportedAccountProviders = []string{ProviderAnthropic, ProviderOpenAI}

// AccountLoginOptions contains parameters for logging into an account provider.
type AccountLoginOptions struct {
	Provider    string
	Output      io.Writer
	Input       io.Reader
	OpenBrowser bool
}

// AccountLogoutOptions contains parameters for logging out from an account provider.
type AccountLogoutOptions struct {
	Provider string
	Output   io.Writer
}

// AccountUsageOptions contains parameters for retrieving account usage.
type AccountUsageOptions struct {
	Provider string
	Output   io.Writer
}

func normalizeAccountProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, supported := range SupportedAccountProviders {
		if provider == supported {
			return provider, nil
		}
	}
	return "", fmt.Errorf("unsupported provider %q (supported: %s)", provider, strings.Join(SupportedAccountProviders, ", "))
}

// AccountLogin logs into the specified account provider.
func AccountLogin(ctx context.Context, opts AccountLoginOptions) error {
	provider, err := normalizeAccountProvider(opts.Provider)
	if err != nil {
		return err
	}

	switch provider {
	case ProviderAnthropic:
		return loginAnthropicAccount(ctx, opts)
	case ProviderOpenAI:
		return loginOpenAIAccount(ctx, opts)
	default:
		return fmt.Errorf("unsupported provider %q (supported: %s)", provider, strings.Join(SupportedAccountProviders, ", "))
	}
}

// AccountLogout removes the stored credential for the specified account provider.
func AccountLogout(ctx context.Context, opts AccountLogoutOptions) error {
	provider, err := normalizeAccountProvider(opts.Provider)
	if err != nil {
		return err
	}

	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}
	if err := store.DeleteCredential(provider); err != nil {
		return fmt.Errorf("removing credential: %w", err)
	}

	fmt.Fprintf(opts.Output, "Successfully logged out from %s\n", provider)
	return nil
}

// AccountUsage prints usage information for the specified account provider.
func AccountUsage(ctx context.Context, opts AccountUsageOptions) error {
	provider, err := normalizeAccountProvider(opts.Provider)
	if err != nil {
		return err
	}

	switch provider {
	case ProviderOpenAI:
		return printOpenAIAccountUsage(ctx, opts.Output)
	case ProviderAnthropic:
		return fmt.Errorf("usage is not yet supported for %s accounts", provider)
	default:
		return fmt.Errorf("unsupported provider %q (supported: %s)", provider, strings.Join(SupportedAccountProviders, ", "))
	}
}

func loginAnthropicAccount(ctx context.Context, opts AccountLoginOptions) error {
	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE challenge: %w", err)
	}

	authURL := auth.BuildAuthURL(challenge, verifier)

	fmt.Fprintln(opts.Output, "Opening browser to authenticate with Anthropic...")
	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "If the browser doesn't open, visit this URL manually:")
	fmt.Fprintln(opts.Output, authURL)
	fmt.Fprintln(opts.Output)

	if opts.OpenBrowser {
		if err := auth.OpenBrowser(ctx, authURL); err != nil {
			fmt.Fprintln(opts.Output, "Note: Could not open browser automatically")
		}
	}

	fmt.Fprintln(opts.Output, "After authorizing, you'll see a page with an authorization code.")
	fmt.Fprint(opts.Output, "Paste the authorization code here: ")

	var code string
	if _, err := fmt.Fscanln(opts.Input, &code); err != nil {
		return fmt.Errorf("reading authorization code: %w", err)
	}
	if code == "" {
		return fmt.Errorf("authorization code cannot be empty")
	}

	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "Exchanging code for tokens...")

	tokenResp, err := auth.ExchangeCode(ctx, code, verifier)
	if err != nil {
		return fmt.Errorf("exchanging code for tokens: %w", err)
	}

	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	cred := auth.TokenToCredential(ProviderAnthropic, tokenResp)
	if err := store.SaveCredential(cred); err != nil {
		return fmt.Errorf("saving credential: %w", err)
	}

	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "✓ Successfully authenticated with Anthropic!")
	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "You can now use your Claude Pro/Max subscription with CPE.")
	fmt.Fprintln(opts.Output, "OAuth credentials will be used automatically when no API key is configured.")
	return nil
}

func loginOpenAIAccount(ctx context.Context, opts AccountLoginOptions) error {
	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE challenge: %w", err)
	}

	state, err := auth.GenerateState()
	if err != nil {
		return fmt.Errorf("generating state: %w", err)
	}

	callbackCtx, callbackCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer callbackCancel()

	resultCh, err := auth.StartCallbackServer(callbackCtx, 1455, state)
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}

	authURL := auth.BuildOpenAIAuthURL(challenge, state)

	fmt.Fprintln(opts.Output, "Opening browser to authenticate with OpenAI...")
	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "If the browser doesn't open, visit this URL manually:")
	fmt.Fprintln(opts.Output, authURL)
	fmt.Fprintln(opts.Output)

	if opts.OpenBrowser {
		if err := auth.OpenBrowser(ctx, authURL); err != nil {
			fmt.Fprintln(opts.Output, "Note: Could not open browser automatically")
		}
	}

	fmt.Fprintln(opts.Output, "Waiting for authentication callback...")

	select {
	case result := <-resultCh:
		if result.Error != "" {
			return fmt.Errorf("authentication failed: %s", result.Error)
		}

		fmt.Fprintln(opts.Output)
		fmt.Fprintln(opts.Output, "Exchanging code for tokens...")

		tokenResp, err := auth.ExchangeCodeOpenAI(ctx, result.Code, verifier)
		if err != nil {
			return fmt.Errorf("exchanging code for tokens: %w", err)
		}

		store, err := auth.NewStore()
		if err != nil {
			return fmt.Errorf("initializing auth store: %w", err)
		}

		cred := auth.TokenToCredential(ProviderOpenAI, tokenResp)
		if err := store.SaveCredential(cred); err != nil {
			return fmt.Errorf("saving credential: %w", err)
		}

		fmt.Fprintln(opts.Output)
		fmt.Fprintln(opts.Output, "✓ Successfully authenticated with OpenAI!")
		fmt.Fprintln(opts.Output)
		fmt.Fprintln(opts.Output, "You can now use your ChatGPT subscription with CPE.")
		fmt.Fprintln(opts.Output, "Configure a model with type: responses and auth_method: oauth to use it.")
		return nil
	case <-callbackCtx.Done():
		return fmt.Errorf("authentication timed out (5 minute limit)")
	}
}

func printOpenAIAccountUsage(ctx context.Context, output io.Writer) error {
	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	cred, err := ensureFreshOpenAIAccountCredential(ctx, store)
	if err != nil {
		return err
	}

	usage, err := auth.FetchOpenAIUsage(ctx, http.DefaultClient, auth.OpenAIUsageURL, cred.AccessToken)
	if err != nil {
		return fmt.Errorf("retrieving openai account usage: %w", err)
	}

	enc := json.NewEncoder(output)
	enc.SetIndent("", "  ")
	if err := enc.Encode(usage); err != nil {
		return fmt.Errorf("writing usage output: %w", err)
	}
	return nil
}

func ensureFreshOpenAIAccountCredential(ctx context.Context, store *auth.Store) (*auth.Credential, error) {
	cred, err := store.GetCredential(ProviderOpenAI)
	if err != nil {
		return nil, fmt.Errorf("getting credential: %w", err)
	}

	if cred.ExpiresAt == 0 || time.Now().Unix() < cred.ExpiresAt-60 {
		return cred, nil
	}
	if cred.RefreshToken == "" {
		return nil, fmt.Errorf("openai account token is expired and no refresh token is available; please run 'cpe account login openai' to re-authenticate")
	}

	tokenResp, err := auth.RefreshAccessTokenOpenAI(ctx, cred.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refreshing openai token: %w", err)
	}

	cred = auth.TokenToCredential(ProviderOpenAI, tokenResp)
	if err := store.SaveCredential(cred); err != nil {
		return nil, fmt.Errorf("saving refreshed openai credential: %w", err)
	}
	return cred, nil
}
