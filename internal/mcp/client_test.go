package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/spachava753/cpe/internal/mcpconfig"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMCPRoundTripperDoesNotRetryHTTPStatus(t *testing.T) {
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
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/mcp", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	resp, err := newMCPRoundTripper(base).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestCreateTransportRemoteHTTPClientsUseTransportWithoutClientTimeout(t *testing.T) {
	tests := []struct {
		name       string
		serverType string
	}{
		{name: "streamable http", serverType: "http"},
		{name: "sse", serverType: "sse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := CreateTransport(context.Background(), mcpconfig.ServerConfig{
				Type: tt.serverType,
				URL:  "http://example.com/mcp",
			})
			if err != nil {
				t.Fatalf("CreateTransport() error = %v", err)
			}

			switch tr := transport.(type) {
			case *mcpsdk.StreamableClientTransport:
				assertRemoteMCPHTTPClient(t, tr.HTTPClient)
			case *mcpsdk.SSEClientTransport:
				assertRemoteMCPHTTPClient(t, tr.HTTPClient)
			default:
				t.Fatalf("transport type = %T, want remote HTTP transport", transport)
			}
		})
	}
}

func assertRemoteMCPHTTPClient(t *testing.T, httpClient *http.Client) {
	t.Helper()
	if httpClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if httpClient.Timeout != 0 {
		t.Fatalf("HTTPClient.Timeout = %v, want 0", httpClient.Timeout)
	}
	if httpClient.Transport == nil {
		t.Fatal("HTTPClient.Transport is nil")
	}
}
