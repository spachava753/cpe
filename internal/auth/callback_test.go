package auth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestGenerateState(t *testing.T) {
	state1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error: %v", err)
	}
	if len(state1) != 32 {
		t.Errorf("expected state length 32, got %d", len(state1))
	}

	state2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error: %v", err)
	}
	if state1 == state2 {
		t.Error("two consecutive GenerateState calls returned the same value")
	}
}

// doGet performs an HTTP GET using a context-aware request
func doGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func TestStartCallbackServer_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	port := 19876
	state := "test-state-12345"
	resultCh, err := StartCallbackServer(ctx, port, state)
	if err != nil {
		t.Fatalf("StartCallbackServer() error: %v", err)
	}

	// Simulate the OAuth callback
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		callbackURL := fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=test-code-abc&state=%s", port, state)
		resp, reqErr := doGet(ctx, callbackURL)
		if reqErr != nil {
			// Don't call t.Errorf from goroutine after test might be done
			return
		}
		resp.Body.Close()
	}()

	select {
	case result := <-resultCh:
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
		if result.Code != "test-code-abc" {
			t.Errorf("expected code 'test-code-abc', got %q", result.Code)
		}
		if result.State != state {
			t.Errorf("expected state %q, got %q", state, result.State)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for callback result")
	}

	// Wait for the HTTP request goroutine to complete before test exits
	<-reqDone
}

func TestStartCallbackServer_StateMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	port := 19877
	state := "expected-state"
	resultCh, err := StartCallbackServer(ctx, port, state)
	if err != nil {
		t.Fatalf("StartCallbackServer() error: %v", err)
	}

	// Send callback with wrong state
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		callbackURL := fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=test-code&state=wrong-state", port)
		resp, reqErr := doGet(ctx, callbackURL)
		if reqErr != nil {
			return
		}
		resp.Body.Close()
	}()

	select {
	case result := <-resultCh:
		if result.Error != "state mismatch" {
			t.Errorf("expected error 'state mismatch', got %q", result.Error)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for callback result")
	}

	<-reqDone
}

func TestStartCallbackServer_OAuthError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	port := 19878
	state := "test-state"
	resultCh, err := StartCallbackServer(ctx, port, state)
	if err != nil {
		t.Fatalf("StartCallbackServer() error: %v", err)
	}

	// Send callback with error
	reqDone := make(chan struct{})
	go func() {
		defer close(reqDone)
		callbackURL := fmt.Sprintf("http://127.0.0.1:%d/auth/callback?error=access_denied&error_description=user+denied", port)
		resp, reqErr := doGet(ctx, callbackURL)
		if reqErr != nil {
			return
		}
		resp.Body.Close()
	}()

	select {
	case result := <-resultCh:
		if result.Error != "access_denied: user denied" {
			t.Errorf("expected error 'access_denied: user denied', got %q", result.Error)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for callback result")
	}

	<-reqDone
}

func TestStartCallbackServer_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

	port := 19879
	_, err := StartCallbackServer(ctx, port, "state")
	if err != nil {
		t.Fatalf("StartCallbackServer() error: %v", err)
	}

	// Cancel context immediately
	cancel()

	// Give time for shutdown
	time.Sleep(1 * time.Second)

	// Server should be shut down - connection should fail
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer checkCancel()
	resp, err := doGet(checkCtx, fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=test&state=state", port))
	if err == nil {
		resp.Body.Close()
		t.Error("expected connection error after context cancellation, but request succeeded")
	}
}
