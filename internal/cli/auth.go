package cli

import (
	"context"
	"fmt"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"

	"github.com/spachava753/cpe/internal/auth"
	"github.com/spachava753/cpe/internal/commandio"
)

// Keep command adapters testable without running authentication operations.
type authActions struct {
	login  func(context.Context, *auth.Store, auth.LoginOptions, commandio.IO) error
	logout func(context.Context, *auth.Store, auth.LogoutOptions, commandio.IO) error
}

func authCmd(parent *cobra.Command, store *auth.Store, options auth.LoginOptions) {
	authCmdWithActions(
		parent,
		store,
		options,
		authActions{
			login:  auth.Login,
			logout: auth.Logout,
		},
	)
}

func authCmdWithActions(parent *cobra.Command, store *auth.Store, options auth.LoginOptions, actions authActions) {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Sign in to subscription providers",
		Long: dedent.Dedent(`
		Sign in to OpenAI with a ChatGPT subscription or save an OpenCode Go API key.

		Credentials share auth.json in CPE's configuration directory (~/.cpe).
		Login and logout update only the selected provider's entry.
		`),
	}
	parent.AddCommand(c)
	authLoginCmd(
		c,
		store,
		options,
		actions.login,
	)
	authLogoutCmd(
		c,
		store,
		options.Path,
		actions.logout,
	)
}

func authLoginCmd(parent *cobra.Command, store *auth.Store, options auth.LoginOptions, run func(context.Context, *auth.Store, auth.LoginOptions, commandio.IO) error) {
	var device bool
	c := &cobra.Command{
		Use:   "login PROVIDER",
		Short: "Sign in to OpenAI (ChatGPT) or OpenCode Go",
		Args:  cobra.ExactArgs(1),
		ValidArgs: []string{
			"openai",
			"opencode",
		},
		Long: dedent.Dedent(`
		Sign in to openai or opencode. Provider names are case-insensitive.

		OpenAI prints a browser link and waits for ChatGPT authorization through a
		callback on 127.0.0.1:1455. Use --device for SSH or a browser on another
		machine; enter the displayed code on OpenAI's sign-in page.
		OpenCode Go prompts for an API key in a masked terminal form.

		Successful login replaces only the selected provider's auth.json entry.
		Failed or canceled login preserves saved credentials. Confirmation goes to
		stdout; sign-in instructions and the API-key form go to stderr.
		`),
		Example: dedent.Dedent(fmt.Sprintf(
			`
        %[1]s login openai
        %[1]s login openai --device
        %[1]s login opencode
		`,
			parent.CommandPath(),
		)),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := auth.ParseProvider(args[0])
			if err != nil {
				return err
			}
			return run(
				cmd.Context(),
				store,
				auth.LoginOptions{
					Provider: provider,
					Path:     options.Path,
					Client:   options.Client,
					Device:   device,
				},
				commandio.IO{
					In:  cmd.InOrStdin(),
					Out: cmd.OutOrStdout(),
					Err: cmd.ErrOrStderr(),
				},
			)
		},
	}
	c.Flags().BoolVar(
		&device,
		"device",
		false,
		"Use OpenAI device-code login for a remote or headless terminal",
	)
	parent.AddCommand(c)
}

func authLogoutCmd(parent *cobra.Command, store *auth.Store, path string, run func(context.Context, *auth.Store, auth.LogoutOptions, commandio.IO) error) {
	c := &cobra.Command{
		Use:   "logout PROVIDER",
		Short: "Remove saved CPE credentials for a provider",
		Args:  cobra.ExactArgs(1),
		ValidArgs: []string{
			"openai",
			"opencode",
		},
		Long: dedent.Dedent(`
		Remove the openai or opencode entry from auth.json.
		Provider names are case-insensitive. Other providers are preserved, and an
		absent entry is harmless. Remote sessions and provider-issued keys remain active.
		Confirmation goes to stdout.
		`),
		Example: dedent.Dedent(fmt.Sprintf(
			`
        %[1]s logout openai
        %[1]s logout opencode
		`,
			parent.CommandPath(),
		)),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := auth.ParseProvider(args[0])
			if err != nil {
				return err
			}
			return run(
				cmd.Context(),
				store,
				auth.LogoutOptions{
					Provider: provider,
					Path:     path,
				},
				commandio.IO{
					In:  cmd.InOrStdin(),
					Out: cmd.OutOrStdout(),
					Err: cmd.ErrOrStderr(),
				},
			)
		},
	}
	parent.AddCommand(c)
}
