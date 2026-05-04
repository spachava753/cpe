package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const callbackPort = 3142

type codeReceiver struct {
	redirectURL string
	authChan    chan *auth.AuthorizationResult
	errChan     chan error
	server      *http.Server
}

func newCodeReceiver(redirectURL string) *codeReceiver {
	return &codeReceiver{
		redirectURL: redirectURL,
		authChan:    make(chan *auth.AuthorizationResult, 1),
		errChan:     make(chan error, 1),
	}
}

func (r *codeReceiver) serveRedirectHandler(listener net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		r.authChan <- &auth.AuthorizationResult{
			Code:  req.URL.Query().Get("code"),
			State: req.URL.Query().Get("state"),
		}
		fmt.Fprint(w, "Authentication successful. You can close this window.")
	})

	r.server = &http.Server{
		Addr:    listener.Addr().String(),
		Handler: mux,
	}
	if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		r.errChan <- err
	}
}

func (r *codeReceiver) getAuthorizationCode(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	redirectAddr := r.redirectURL
	if redirectAddr == "" {
		redirectAddr = mcpOAuthRedirectURL()
	}
	parsedRedirect, err := url.Parse(redirectAddr)
	if err != nil {
		return nil, fmt.Errorf("parsing OAuth redirect URL: %w", err)
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", parsedRedirect.Host)
	if err != nil {
		return nil, fmt.Errorf("starting OAuth callback listener: %w", err)
	}
	go r.serveRedirectHandler(listener)
	defer listener.Close()
	defer r.close()

	fmt.Printf("Please open the following URL in your browser: %s\n", args.URL)
	select {
	case authRes := <-r.authChan:
		return authRes, nil
	case err := <-r.errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *codeReceiver) close() {
	if r.server != nil {
		r.server.Close()
	}
}

func mcpOAuthRedirectURL() string {
	return fmt.Sprintf("http://localhost:%d", callbackPort)
}

func newMCPAuthorizationCodeHandler(
	httpClient *http.Client,
	authorizationCodeFetcher auth.AuthorizationCodeFetcher,
) (*auth.AuthorizationCodeHandler, error) {
	redirectURL := mcpOAuthRedirectURL()
	return auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:   "CPE",
				RedirectURIs: []string{redirectURL},
			},
		},
		RedirectURL:              redirectURL,
		AuthorizationCodeFetcher: authorizationCodeFetcher,
		Client:                   httpClient,
	})
}
