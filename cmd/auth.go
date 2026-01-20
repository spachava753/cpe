package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/commands"
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

Environment variables for Anthropic OAuth configuration:
  CPE_ANTHROPIC_CLIENT_ID    - OAuth client ID
  CPE_ANTHROPIC_AUTH_URL     - Authorization URL (default: https://claude.ai/oauth/authorize)
  CPE_ANTHROPIC_TOKEN_URL    - Token exchange URL (default: https://console.anthropic.com/v1/oauth/token)
  CPE_ANTHROPIC_REDIRECT_URI - Redirect URI (default: https://console.anthropic.com/oauth/code/callback)
  CPE_ANTHROPIC_SCOPES       - OAuth scopes (default: org:create_api_key user:profile user:inference)

These environment variables allow overriding the default OAuth endpoints, which can be
useful for testing or when using alternative authentication servers.

Example:
  cpe auth login anthropic
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		switch provider {
		case "anthropic":
			return commands.AuthLoginAnthropic(cmd.Context(), commands.AuthLoginAnthropicOptions{
				Output:      os.Stdout,
				Input:       os.Stdin,
				OpenBrowser: true,
			})
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

var authRefreshCmd = &cobra.Command{
	Use:   "refresh [provider]",
	Short: "Refresh OAuth tokens for a provider",
	Long: `Force refresh OAuth tokens for an AI provider, even if the current token hasn't expired.

This is useful when you want to proactively refresh tokens before they expire,
or if you suspect the current token may be invalid.

Example:
  cpe auth refresh anthropic
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		switch provider {
		case "anthropic":
			return commands.AuthRefreshAnthropic(cmd.Context(), commands.AuthRefreshAnthropicOptions{
				Output: os.Stdout,
			})
		default:
			return fmt.Errorf("unsupported provider %q (supported: anthropic)", provider)
		}
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

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authRefreshCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
