package subagentlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client sends events to the event server via HTTP POST requests
type Client struct {
	address string
	client  *http.Client
}

// NewClient creates a new event client that sends events to the given address
func NewClient(address string) *Client {
	return &Client{
		address: address,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Emit sends an event to the server. It returns an error on connection failure,
// non-2xx response, or timeout. Per spec, failures are fatal and the caller
// should abort the subagent on emission failure.
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
