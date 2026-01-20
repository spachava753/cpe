package commands

import (
	"context"
	"fmt"
	"io"

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

	cred := auth.TokenToCredential(tokenResp)
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

	newCred := auth.TokenToCredential(tokenResp)
	if err := store.SaveCredential(newCred); err != nil {
		return fmt.Errorf("saving credential: %w", err)
	}

	fmt.Fprintln(opts.Output, "✓ Successfully refreshed Anthropic OAuth token!")
	return nil
}
