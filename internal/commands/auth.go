package commands

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spachava753/cpe/internal/auth"
)

// AuthLoginAnthropicOptions contains parameters for Anthropic OAuth login
type AuthLoginAnthropicOptions struct {
	// Output is where to write status messages (e.g., os.Stdout)
	Output io.Writer
	// Input is where to read user input from (e.g., os.Stdin)
	Input io.Reader
	// OpenBrowser controls whether to attempt opening the browser
	OpenBrowser bool
}

// AuthLoginAnthropic performs the OAuth login flow for Anthropic
func AuthLoginAnthropic(ctx context.Context, opts AuthLoginAnthropicOptions) error {
	// Generate PKCE challenge
	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE challenge: %w", err)
	}

	// Build authorization URL (state is set to verifier per Anthropic's OAuth)
	authURL := auth.BuildAuthURL(challenge, verifier)

	fmt.Fprintln(opts.Output, "Opening browser to authenticate with Anthropic...")
	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "If the browser doesn't open, visit this URL manually:")
	fmt.Fprintln(opts.Output, authURL)
	fmt.Fprintln(opts.Output)

	// Try to open browser
	if opts.OpenBrowser {
		if err := auth.OpenBrowser(ctx, authURL); err != nil {
			fmt.Fprintln(opts.Output, "Note: Could not open browser automatically")
		}
	}

	fmt.Fprintln(opts.Output, "After authorizing, you'll see a page with an authorization code.")
	fmt.Fprint(opts.Output, "Paste the authorization code here: ")

	// Read the authorization code from input
	var code string
	if _, err := fmt.Fscanln(opts.Input, &code); err != nil {
		return fmt.Errorf("reading authorization code: %w", err)
	}

	if code == "" {
		return fmt.Errorf("authorization code cannot be empty")
	}

	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "Exchanging code for tokens...")

	// Exchange the code for tokens
	tokenResp, err := auth.ExchangeCode(ctx, code, verifier)
	if err != nil {
		return fmt.Errorf("exchanging code for tokens: %w", err)
	}

	// Store the credential
	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	cred := auth.TokenToCredential("anthropic", tokenResp)
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

// AuthRefreshAnthropicOptions contains parameters for Anthropic token refresh
type AuthRefreshAnthropicOptions struct {
	// Output is where to write status messages
	Output io.Writer
}

// AuthRefreshAnthropic refreshes the OAuth token for Anthropic
func AuthRefreshAnthropic(ctx context.Context, opts AuthRefreshAnthropicOptions) error {
	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	cred, err := store.GetCredential("anthropic")
	if err != nil {
		return fmt.Errorf("getting credential: %w", err)
	}

	if cred.RefreshToken == "" {
		return fmt.Errorf("no refresh token available; please run 'cpe auth login anthropic' to re-authenticate")
	}

	fmt.Fprintln(opts.Output, "Refreshing Anthropic OAuth token...")

	tokenResp, err := auth.RefreshAccessToken(ctx, cred.RefreshToken)
	if err != nil {
		return fmt.Errorf("refreshing token: %w", err)
	}

	newCred := auth.TokenToCredential("anthropic", tokenResp)
	if err := store.SaveCredential(newCred); err != nil {
		return fmt.Errorf("saving credential: %w", err)
	}

	fmt.Fprintln(opts.Output, "✓ Successfully refreshed Anthropic OAuth token!")
	return nil
}

// AuthLogoutOptions contains parameters for logging out from a provider
type AuthLogoutOptions struct {
	// Provider is the provider to log out from
	Provider string
	// Output is where to write status messages
	Output io.Writer
}

// AuthLoginOpenAIOptions contains parameters for OpenAI OAuth login
type AuthLoginOpenAIOptions struct {
	// Output is where to write status messages
	Output io.Writer
	// OpenBrowser controls whether to attempt opening the browser
	OpenBrowser bool
}

// AuthLoginOpenAI performs the OAuth login flow for OpenAI using a local callback server.
// It starts a local HTTP server on port 1455 to receive the OAuth callback.
func AuthLoginOpenAI(ctx context.Context, opts AuthLoginOpenAIOptions) error {
	// Generate PKCE challenge
	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE challenge: %w", err)
	}

	// Generate state parameter
	state, err := auth.GenerateState()
	if err != nil {
		return fmt.Errorf("generating state: %w", err)
	}

	// Start local callback server
	callbackCtx, callbackCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer callbackCancel()

	resultCh, err := auth.StartCallbackServer(callbackCtx, 1455, state)
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}

	// Build authorization URL
	authURL := auth.BuildOpenAIAuthURL(challenge, state)

	fmt.Fprintln(opts.Output, "Opening browser to authenticate with OpenAI...")
	fmt.Fprintln(opts.Output)
	fmt.Fprintln(opts.Output, "If the browser doesn't open, visit this URL manually:")
	fmt.Fprintln(opts.Output, authURL)
	fmt.Fprintln(opts.Output)

	// Try to open browser
	if opts.OpenBrowser {
		if err := auth.OpenBrowser(ctx, authURL); err != nil {
			fmt.Fprintln(opts.Output, "Note: Could not open browser automatically")
		}
	}

	fmt.Fprintln(opts.Output, "Waiting for authentication callback...")

	// Wait for callback
	select {
	case result := <-resultCh:
		if result.Error != "" {
			return fmt.Errorf("authentication failed: %s", result.Error)
		}

		fmt.Fprintln(opts.Output)
		fmt.Fprintln(opts.Output, "Exchanging code for tokens...")

		// Exchange the code for tokens
		tokenResp, err := auth.ExchangeCodeOpenAI(ctx, result.Code, verifier)
		if err != nil {
			return fmt.Errorf("exchanging code for tokens: %w", err)
		}

		// Store the credential
		store, err := auth.NewStore()
		if err != nil {
			return fmt.Errorf("initializing auth store: %w", err)
		}

		cred := auth.TokenToCredential("openai", tokenResp)
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

// AuthRefreshOpenAIOptions contains parameters for OpenAI token refresh
type AuthRefreshOpenAIOptions struct {
	// Output is where to write status messages
	Output io.Writer
}

// AuthRefreshOpenAI refreshes the OAuth token for OpenAI
func AuthRefreshOpenAI(ctx context.Context, opts AuthRefreshOpenAIOptions) error {
	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	cred, err := store.GetCredential("openai")
	if err != nil {
		return fmt.Errorf("getting credential: %w", err)
	}

	if cred.RefreshToken == "" {
		return fmt.Errorf("no refresh token available; please run 'cpe auth login openai' to re-authenticate")
	}

	fmt.Fprintln(opts.Output, "Refreshing OpenAI OAuth token...")

	tokenResp, err := auth.RefreshAccessTokenOpenAI(ctx, cred.RefreshToken)
	if err != nil {
		return fmt.Errorf("refreshing token: %w", err)
	}

	newCred := auth.TokenToCredential("openai", tokenResp)
	if err := store.SaveCredential(newCred); err != nil {
		return fmt.Errorf("saving credential: %w", err)
	}

	fmt.Fprintln(opts.Output, "✓ Successfully refreshed OpenAI OAuth token!")
	return nil
}

// SupportedProviders is the list of providers that support OAuth authentication
var SupportedProviders = []string{"anthropic", "openai"}

// AuthLogout removes stored credentials for a provider
func AuthLogout(ctx context.Context, opts AuthLogoutOptions) error {
	// Validate provider
	supported := false
	for _, p := range SupportedProviders {
		if opts.Provider == p {
			supported = true
			break
		}
	}
	if !supported {
		return fmt.Errorf("unsupported provider %q (supported: %s)", opts.Provider, strings.Join(SupportedProviders, ", "))
	}

	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	if err := store.DeleteCredential(opts.Provider); err != nil {
		return fmt.Errorf("removing credential: %w", err)
	}

	fmt.Fprintf(opts.Output, "Successfully logged out from %s\n", opts.Provider)
	return nil
}

// AuthStatusOptions contains parameters for showing auth status
type AuthStatusOptions struct {
	// Output is where to write status messages
	Output io.Writer
}

// ProviderStatus represents the authentication status for a provider
type ProviderStatus struct {
	Provider string
	Status   string
	Error    error
}

// AuthStatus returns the authentication status for all providers
func AuthStatus(ctx context.Context, opts AuthStatusOptions) error {
	store, err := auth.NewStore()
	if err != nil {
		return fmt.Errorf("initializing auth store: %w", err)
	}

	providers, err := store.ListCredentials()
	if err != nil {
		return fmt.Errorf("listing credentials: %w", err)
	}

	if len(providers) == 0 {
		fmt.Fprintln(opts.Output, "No providers authenticated")
		fmt.Fprintln(opts.Output)
		fmt.Fprintln(opts.Output, "To authenticate with Anthropic:")
		fmt.Fprintln(opts.Output, "  cpe auth login anthropic")
		return nil
	}

	fmt.Fprintln(opts.Output, "Authenticated providers:")
	for _, p := range providers {
		cred, err := store.GetCredential(p)
		if err != nil {
			fmt.Fprintf(opts.Output, "  %s: error reading credential\n", p)
			continue
		}

		status := "valid"
		if cred.IsExpired() {
			status = "expired (will refresh on next use)"
		} else if cred.ExpiresAt > 0 {
			remaining := time.Until(time.Unix(cred.ExpiresAt, 0))
			status = fmt.Sprintf("valid (expires in %s)", remaining.Round(time.Minute))
		}

		fmt.Fprintf(opts.Output, "  %s: %s\n", p, status)
	}

	return nil
}
