package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/auth"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for AI providers",
	Long:  "Manage OAuth authentication for AI providers like Anthropic Claude Pro/Max subscriptions.",
}

var authLoginCmd = &cobra.Command{
	Use:   "login [provider]",
	Short: "Authenticate with an AI provider via OAuth",
	Long: `Start an OAuth flow to authenticate with an AI provider.

Currently supported providers:
  - anthropic: Authenticate with Claude Pro/Max subscription

Example:
  cpe auth login anthropic
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		switch provider {
		case "anthropic":
			return loginAnthropic(cmd)
		default:
			return fmt.Errorf("unsupported provider %q (supported: anthropic)", provider)
		}
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout [provider]",
	Short: "Remove stored credentials for a provider",
	Long: `Remove stored OAuth credentials for an AI provider.

Example:
  cpe auth logout anthropic
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])

		store, err := auth.NewStore()
		if err != nil {
			return fmt.Errorf("initializing auth store: %w", err)
		}

		if err := store.DeleteCredential(provider); err != nil {
			return fmt.Errorf("removing credential: %w", err)
		}

		fmt.Printf("Successfully logged out from %s\n", provider)
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status for all providers",
	Long: `Display the current authentication status for all configured providers.

Example:
  cpe auth status
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := auth.NewStore()
		if err != nil {
			return fmt.Errorf("initializing auth store: %w", err)
		}

		providers, err := store.ListCredentials()
		if err != nil {
			return fmt.Errorf("listing credentials: %w", err)
		}

		if len(providers) == 0 {
			fmt.Println("No providers authenticated")
			fmt.Println("\nTo authenticate with Anthropic:")
			fmt.Println("  cpe auth login anthropic")
			return nil
		}

		fmt.Println("Authenticated providers:")
		for _, p := range providers {
			cred, err := store.GetCredential(p)
			if err != nil {
				fmt.Printf("  %s: error reading credential\n", p)
				continue
			}

			status := "valid"
			if cred.IsExpired() {
				status = "expired (will refresh on next use)"
			} else if cred.ExpiresAt > 0 {
				remaining := time.Until(time.Unix(cred.ExpiresAt, 0))
				status = fmt.Sprintf("valid (expires in %s)", remaining.Round(time.Minute))
			}

			fmt.Printf("  %s: %s\n", p, status)
		}

		return nil
	},
}

func loginAnthropic(cmd *cobra.Command) error {
	// Generate PKCE challenge
	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE challenge: %w", err)
	}

	// Build authorization URL (state is set to verifier per Anthropic's OAuth)
	authURL := auth.BuildAuthURL(challenge, verifier)

	fmt.Println("Opening browser to authenticate with Anthropic...")
	fmt.Println()
	fmt.Println("If the browser doesn't open, visit this URL manually:")
	fmt.Println(authURL)
	fmt.Println()

	// Try to open browser
	if err := auth.OpenBrowser(authURL); err != nil {
		fmt.Println("Note: Could not open browser automatically")
	}

	fmt.Println("After authorizing, you'll see a page with an authorization code.")
	fmt.Print("Paste the authorization code here: ")

	// Read the authorization code from stdin
	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading authorization code: %w", err)
	}
	code = strings.TrimSpace(code)

	if code == "" {
		return fmt.Errorf("authorization code cannot be empty")
	}

	// Pass the full code#state to ExchangeCode which will parse it

	fmt.Println()
	fmt.Println("Exchanging code for tokens...")

	// Exchange the code for tokens
	tokenResp, err := auth.ExchangeCode(cmd.Context(), code, verifier)
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

	fmt.Println()
	fmt.Println("✓ Successfully authenticated with Anthropic!")
	fmt.Println()
	fmt.Println("You can now use your Claude Pro/Max subscription with CPE.")
	fmt.Println("OAuth credentials will be used automatically when no API key is configured.")

	return nil
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
