package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spachava753/cpe/internal/commandio"
)

// LoginOptions selects a provider, credential file, and sign-in flow.
type LoginOptions struct {
	Provider Provider
	// Path is the native path to the shared auth.json file.
	Path string
	// Client performs OpenAI authentication requests and is required for OpenAI login.
	Client *http.Client
	// Device uses OpenAI's device-code flow for remote or headless terminals.
	Device bool
}

type loginFlow struct {
	oauth     openAIClient
	promptKey func(context.Context, commandio.IO) (string, error)
}

// Login signs in, saves the provider's credentials at opts.Path, and writes a confirmation.
// Failed sign-in leaves credentials unchanged; output errors may follow a successful save.
func Login(ctx context.Context, store *Store, opts LoginOptions, streams commandio.IO) error {
	return login(
		ctx,
		store,
		opts,
		streams,
		loginFlow{
			oauth: openAIClient{
				client:  opts.Client,
				issuer:  "https://auth.openai.com",
				address: "127.0.0.1:1455",
			},
			promptKey: promptAPIKey,
		},
	)
}

func login(ctx context.Context, store *Store, opts LoginOptions, streams commandio.IO, flow loginFlow) error {
	if err := store.configured(opts.Path); err != nil {
		return err
	}
	if _, err := opts.Provider.name(); err != nil {
		return err
	}
	if opts.Device && opts.Provider != OpenAI {
		return errors.New("device login is only available for openai")
	}
	if opts.Provider == OpenAI && (flow.oauth.client == nil || flow.oauth.issuer == "" || (!opts.Device && flow.oauth.address == "")) {
		return errors.New("OpenAI login requires an HTTP client and authorization endpoints")
	}
	if opts.Provider == OpenCode && flow.promptKey == nil {
		return errors.New("OpenCode login requires an API-key prompt")
	}
	if _, err := store.Retrieve(
		ctx,
		opts.Path,
	); err != nil {
		return err
	}
	var credentials Credentials
	var err error
	switch opts.Provider {
	case OpenAI:
		var tokens OAuthCredentials
		if opts.Device {
			tokens, err = flow.oauth.deviceLogin(
				ctx,
				streams.ErrorOutput(),
			)
		} else {
			tokens, err = flow.oauth.browserLogin(
				ctx,
				streams.ErrorOutput(),
			)
		}
		if err == nil {
			credentials.OpenAI = &tokens
		}
	case OpenCode:
		var key string
		key, err = flow.promptKey(
			ctx,
			streams,
		)
		if err == nil {
			key = strings.TrimSpace(key)
			err = validateAPIKey(key)
		}
		if err == nil {
			credentials.OpenCode = &APIKeyCredentials{
				APIKey: key,
			}
		}
	}
	if err != nil {
		return fmt.Errorf(
			"login: %w",
			err,
		)
	}
	if err := store.Save(
		ctx,
		opts.Path,
		opts.Provider,
		credentials,
	); err != nil {
		return err
	}
	name, err := opts.Provider.name()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		streams.Output(),
		"Saved %s credentials.\n",
		name,
	)
	return err
}
