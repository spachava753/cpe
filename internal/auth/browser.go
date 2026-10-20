package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

type authorizationResult struct {
	code string
	err  error
}

func (client openAIClient) browserLogin(ctx context.Context, output io.Writer) (OAuthCredentials, error) {
	if client.client == nil || client.issuer == "" || client.address == "" {
		return OAuthCredentials{}, errors.New("OpenAI browser login requires an HTTP client, issuer, and callback address")
	}
	ctx, cancel := context.WithTimeout(
		ctx,
		loginTimeout,
	)
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(
		ctx,
		"tcp4",
		client.address,
	)
	if err != nil {
		return OAuthCredentials{}, errors.New("listen for OpenAI login on 127.0.0.1:1455; close other login attempts or use --device")
	}
	defer listener.Close()
	verifier := rand.Text() + rand.Text()
	state := rand.Text()
	digest := sha256.Sum256([]byte(verifier))
	redirect := "http://" + listener.Addr().String() + "/auth/callback"
	parameters := url.Values{
		"response_type": {
			"code",
		},
		clientIDParameter: {
			openAIClientID,
		},
		"redirect_uri": {
			redirect,
		},
		"scope": {
			"openid profile email offline_access",
		},
		"code_challenge": {
			base64.RawURLEncoding.EncodeToString(digest[:]),
		},
		"code_challenge_method": {
			"S256",
		},
		"state": {
			state,
		},
		"id_token_add_organizations": {
			"true",
		},
		"codex_cli_simplified_flow": {
			"true",
		},
		"originator": {
			"cpe",
		},
	}
	results := make(
		chan authorizationResult,
		1,
	)
	server := &http.Server{
		Handler: callbackHandler(
			state,
			results,
		),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    16 * 1024,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	defer server.Close()
	serverErrors := make(
		chan error,
		1,
	)
	go func() {
		serverErrors <- server.Serve(listener)
	}()
	if _, err := fmt.Fprintf(
		output,
		"Open this link in a browser on this machine:\n%s/oauth/authorize?%s\nWaiting for ChatGPT authorization (Ctrl+C cancels).\nFor a remote terminal, use cpe auth login openai --device.\n",
		client.issuer,
		parameters.Encode(),
	); err != nil {
		return OAuthCredentials{}, fmt.Errorf(
			"display login link: %w",
			err,
		)
	}
	select {
	case result := <-results:
		if result.err != nil {
			return OAuthCredentials{}, result.err
		}
		return client.exchange(
			ctx,
			result.code,
			verifier,
			redirect,
		)
	case <-serverErrors:
		return OAuthCredentials{}, errors.New("OpenAI login callback server stopped")
	case <-ctx.Done():
		return OAuthCredentials{}, fmt.Errorf(
			"wait for OpenAI authorization: %w",
			ctx.Err(),
		)
	}
}

func callbackHandler(state string, results chan<- authorizationResult) http.Handler {
	var consumed atomic.Bool
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(
			"Cache-Control",
			"no-store",
		)
		writer.Header().Set(
			"Content-Type",
			"text/plain; charset=utf-8",
		)
		writer.Header().Set(
			"X-Content-Type-Options",
			"nosniff",
		)
		writer.Header().Set(
			"Referrer-Policy",
			"no-referrer",
		)
		if request.URL.Path != "/auth/callback" {
			http.NotFound(
				writer,
				request,
			)
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set(
				"Allow",
				http.MethodGet,
			)
			http.Error(
				writer,
				"Use GET.",
				http.StatusMethodNotAllowed,
			)
			return
		}
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil || len(query["state"]) != 1 || subtle.ConstantTimeCompare(
			[]byte(query.Get("state")),
			[]byte(state),
		) != 1 {
			http.Error(
				writer,
				"Invalid authorization state.",
				http.StatusBadRequest,
			)
			return
		}
		result := authorizationResult{}
		switch {
		case len(query["error"]) == 1 && len(query["code"]) == 0:
			result.err = errors.New("OpenAI authorization was denied; retry signing in")
		case len(query["code"]) == 1 && query.Get("code") != "" && len(query["error"]) == 0:
			result.code = query.Get("code")
		default:
			http.Error(
				writer,
				"Incomplete authorization response.",
				http.StatusBadRequest,
			)
			return
		}
		if !consumed.CompareAndSwap(
			false,
			true,
		) {
			http.Error(
				writer,
				"Authorization already received.",
				http.StatusConflict,
			)
			return
		}
		if result.err != nil {
			http.Error(
				writer,
				"Authorization was denied. Return to CPE.",
				http.StatusBadRequest,
			)
		} else {
			_, _ = io.WriteString(
				writer,
				"Authorization received. Return to CPE to finish signing in.\n",
			)
		}
		results <- result
	})
}
