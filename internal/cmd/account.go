package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/commands"
)

var (
	accountUsageRaw   bool
	accountUsageWatch bool
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage AI provider accounts",
	Long:  "Manage AI provider accounts, including login, logout, and subscription usage lookups.",
}

var accountLoginCmd = &cobra.Command{
	Use:   "login [provider]",
	Short: "Log into an AI provider account",
	Long: `Start an OAuth flow to log into an AI provider account.

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
  cpe account login anthropic
  cpe account login openai
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.AccountLogin(cmd.Context(), commands.AccountLoginOptions{
			Provider:    strings.ToLower(args[0]),
			Output:      cmd.OutOrStdout(),
			Input:       cmd.InOrStdin(),
			OpenBrowser: true,
		})
	},
}

var accountLogoutCmd = &cobra.Command{
	Use:   "logout [provider]",
	Short: "Remove stored credentials for an account",
	Long: `Remove stored account credentials for an AI provider.

Example:
  cpe account logout anthropic
  cpe account logout openai
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.AccountLogout(cmd.Context(), commands.AccountLogoutOptions{
			Provider: strings.ToLower(args[0]),
			Output:   cmd.OutOrStdout(),
		})
	},
}

var accountUsageCmd = &cobra.Command{
	Use:   "usage [provider]",
	Short: "Show account usage for a provider",
	Long: `Show account usage information for an AI provider.

By default, CPE renders a compact usage view with the primary 5-hour and
secondary weekly windows. Use --watch to keep refreshing the display live, or
--raw to print the original JSON response for scripts and analytics.

Currently supported providers:
  - openai: Usage and rate-limit information from the ChatGPT account backend

Examples:
  cpe account usage openai
  cpe account usage openai --watch
  cpe account usage openai --raw
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.AccountUsage(cmd.Context(), commands.AccountUsageOptions{
			Provider: strings.ToLower(args[0]),
			Output:   cmd.OutOrStdout(),
			Raw:      accountUsageRaw,
			Watch:    accountUsageWatch,
		})
	},
}

func init() {
	accountUsageCmd.Flags().BoolVar(&accountUsageRaw, "raw", false, "Print the raw JSON usage response")
	accountUsageCmd.Flags().BoolVarP(&accountUsageWatch, "watch", "W", false, "Refresh and watch usage live")

	accountCmd.AddCommand(accountLoginCmd)
	accountCmd.AddCommand(accountLogoutCmd)
	accountCmd.AddCommand(accountUsageCmd)
	rootCmd.AddCommand(accountCmd)
}
