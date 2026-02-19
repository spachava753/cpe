package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

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
  - openai:    Authenticate with ChatGPT Plus/Pro/Team subscription

Environment variables for Anthropic OAuth configuration:
  CPE_ANTHROPIC_CLIENT_ID    - OAuth client ID
  CPE_ANTHROPIC_AUTH_URL     - Authorization URL (default: https://claude.ai/oauth/authorize)
  CPE_ANTHROPIC_TOKEN_URL    - Token exchange URL (default: https://console.anthropic.com/v1/oauth/token)
  CPE_ANTHROPIC_REDIRECT_URI - Redirect URI (default: https://console.anthropic.com/oauth/code/callback)
  CPE_ANTHROPIC_SCOPES       - OAuth scopes (default: org:create_api_key user:profile user:inference)

Environment variables for OpenAI OAuth configuration:
  CPE_OPENAI_CLIENT_ID    - OAuth client ID
  CPE_OPENAI_AUTH_URL     - Authorization URL (default: https://auth.openai.com/oauth/authorize)
  CPE_OPENAI_TOKEN_URL    - Token exchange URL (default: https://auth.openai.com/oauth/token)
  CPE_OPENAI_REDIRECT_URI - Redirect URI (default: http://localhost:1455/auth/callback)
  CPE_OPENAI_SCOPES       - OAuth scopes (default: openid profile email offline_access)

These environment variables allow overriding the default OAuth endpoints, which can be
useful for testing or when using alternative authentication servers.

Example:
  cpe auth login anthropic
  cpe auth login openai
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
		case "openai":
			return commands.AuthLoginOpenAI(cmd.Context(), commands.AuthLoginOpenAIOptions{
				Output:      os.Stdout,
				OpenBrowser: true,
			})
		default:
			return fmt.Errorf("unsupported provider %q (supported: anthropic, openai)", provider)
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
		return commands.AuthLogout(cmd.Context(), commands.AuthLogoutOptions{
			Provider: provider,
			Output:   os.Stdout,
		})
	},
}

var authRefreshCmd = &cobra.Command{
	Use:   "refresh [provider]",
	Short: "Refresh OAuth tokens for a provider",
	Long: `Force refresh OAuth tokens for an AI provider, even if the current token hasn't expired.

This is useful when you want to proactively refresh tokens before they expire,
or if you suspect the current token may be invalid.

Environment variables for Anthropic OAuth configuration:
  CPE_ANTHROPIC_CLIENT_ID    - OAuth client ID
  CPE_ANTHROPIC_TOKEN_URL    - Token exchange URL (default: https://console.anthropic.com/v1/oauth/token)
  CPE_ANTHROPIC_REDIRECT_URI - Redirect URI (default: https://console.anthropic.com/oauth/code/callback)

Environment variables for OpenAI OAuth configuration:
  CPE_OPENAI_CLIENT_ID    - OAuth client ID
  CPE_OPENAI_TOKEN_URL    - Token exchange URL (default: https://auth.openai.com/oauth/token)
  CPE_OPENAI_REDIRECT_URI - Redirect URI (default: http://localhost:1455/auth/callback)

Example:
  cpe auth refresh anthropic
  cpe auth refresh openai
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		switch provider {
		case "anthropic":
			return commands.AuthRefreshAnthropic(cmd.Context(), commands.AuthRefreshAnthropicOptions{
				Output: os.Stdout,
			})
		case "openai":
			return commands.AuthRefreshOpenAI(cmd.Context(), commands.AuthRefreshOpenAIOptions{
				Output: os.Stdout,
			})
		default:
			return fmt.Errorf("unsupported provider %q (supported: anthropic, openai)", provider)
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
		return commands.AuthStatus(cmd.Context(), commands.AuthStatusOptions{
			Output: os.Stdout,
		})
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authRefreshCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
