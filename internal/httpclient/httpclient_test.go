package httpclient

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error {
	c.closed = true
	return nil
}

func TestNewPreservesBaseClientAndAppliesDefaultTimeout(t *testing.T) {
	base := &http.Client{
		Timeout: 7 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("stop")
		},
	}

	client := New(WithBaseClient(base), WithDefaultTimeout(30*time.Second))
	if client == base {
		t.Fatal("New returned the base client instead of a copy")
	}
	if client.Timeout != 7*time.Second {
		t.Fatalf("Timeout = %v, want preserved base timeout", client.Timeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect was not preserved")
	}
	if client.Transport == nil {
		t.Fatal("Transport is nil")
	}
}

func TestTransportWithRetryStatusesFalseDoesNotRetryHTTPStatus(t *testing.T) {
	attempts := 0
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     http.StatusText(http.StatusServiceUnavailable),
			Body:       io.NopCloser(bytes.NewBufferString("unavailable")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	client := New(
		WithBaseTransport(base),
		WithRetryStatuses(false),
		WithMaxRetries(2),
		WithBackoff(time.Millisecond, time.Millisecond),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestTransportWithRetryStatusesFalseRetriesTransportError(t *testing.T) {
	attempts := 0
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("connection reset by peer")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	client := New(
		WithBaseTransport(base),
		WithRetryStatuses(false),
		WithMaxRetries(1),
		WithBackoff(time.Millisecond, time.Millisecond),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestTransportRetriesAndClosesIntermediateResponseBody(t *testing.T) {
	attempts := 0
	firstBody := &closeTracker{Reader: bytes.NewBufferString("try again")}
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		status := http.StatusOK
		body := io.NopCloser(bytes.NewBufferString("ok"))
		if attempts == 1 {
			status = http.StatusServiceUnavailable
			body = firstBody
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       body,
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	client := New(
		WithBaseTransport(base),
		WithMaxRetries(1),
		WithBackoff(time.Millisecond, time.Millisecond),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if !firstBody.closed {
		t.Fatal("retry response body was not closed")
	}
}
