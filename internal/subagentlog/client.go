package subagentlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client emits subagent logging events to the local parent-process HTTP server.
// It is intentionally fail-fast: callers treat emission errors as fatal for
// observability-sensitive events.
type Client struct {
	address string
	client  *http.Client
}

// NewClient creates an event client for the given base address
// (for example, "http://127.0.0.1:12345").
//
// Requests use a fixed 5-second HTTP timeout so connection failures, refused
// connections, and hung handlers surface quickly and can abort subagent runs.
func NewClient(address string) *Client {
	return &Client{
		address: address,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Emit posts one Event to /subagent-events as JSON.
//
// Contract:
//   - Any marshal/request/transport error is returned.
//   - Any non-2xx status is returned as an error.
//   - No retries are performed by this client.
//
// Per subagent-logging spec, callers should treat a returned error as fatal for
// in-flight execution and abort rather than continue without observability.
func (c *Client) Emit(ctx context.Context, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.address+"/subagent-events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-2xx status: %d %s", resp.StatusCode, resp.Status)
	}

	return nil
}
