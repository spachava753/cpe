package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"
)

// CallbackResult holds the result from an OAuth callback server
type CallbackResult struct {
	Code  string
	State string
	Error string
}

// GenerateState generates a random state string for OAuth
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// StartCallbackServer starts a local HTTP server that listens for the OAuth callback.
// It returns the authorization code via the result channel.
// The server shuts down after receiving the callback or when the context is cancelled.
func StartCallbackServer(ctx context.Context, port int, expectedState string) (<-chan CallbackResult, error) {
	resultCh := make(chan CallbackResult, 1)

	// doneCh is used solely to signal that a result has been written,
	// so the shutdown goroutine knows to stop the server without
	// consuming the actual result from resultCh.
	doneCh := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			errDesc := r.URL.Query().Get("error_description")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><h1>Authentication Failed</h1><p>%s: %s</p><p>You can close this window.</p></body></html>`,
				html.EscapeString(errParam), html.EscapeString(errDesc))
			resultCh <- CallbackResult{Error: fmt.Sprintf("%s: %s", errParam, errDesc)}
			close(doneCh)
			return
		}

		if state != expectedState {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h1>Authentication Failed</h1><p>State mismatch.</p><p>You can close this window.</p></body></html>`)
			resultCh <- CallbackResult{Error: "state mismatch"}
			close(doneCh)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h1>Authentication Successful!</h1><p>You can close this window and return to the terminal.</p></body></html>`)
		resultCh <- CallbackResult{Code: code, State: state}
		close(doneCh)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", server.Addr)
	if err != nil {
		return nil, fmt.Errorf("starting callback server on port %d: %w", port, err)
	}

	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			resultCh <- CallbackResult{Error: fmt.Sprintf("callback server error: %v", err)}
			close(doneCh)
		}
	}()

	// Shut down server after callback is received or context is cancelled.
	// Uses doneCh instead of reading from resultCh to avoid consuming the result.
	go func() {
		select {
		case <-ctx.Done():
		case <-doneCh:
			// Give a moment for the HTTP response to be flushed to the browser
			time.Sleep(500 * time.Millisecond)
		}
		// Intentionally using a fresh context for shutdown since the parent
		// context may already be cancelled.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx) //nolint:contextcheck
	}()

	return resultCh, nil
}
