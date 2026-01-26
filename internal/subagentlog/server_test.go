package subagentlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/bradleyjkemp/cupaloy/v2"
)

func TestServer(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   any
	}{
		{
			name:   "valid POST with event",
			method: http.MethodPost,
			body: Event{
				SubagentName:  "test-agent",
				SubagentRunID: "run-123",
				Timestamp:     time.Now(),
				Type:          EventTypeToolCall,
				ToolName:      "test-tool",
			},
		},
		{
			name:   "valid POST with minimal event",
			method: http.MethodPost,
			body: Event{
				SubagentName:  "minimal",
				SubagentRunID: "run-456",
				Type:          EventTypeSubagentStart,
			},
		},
		{
			name:   "invalid JSON",
			method: http.MethodPost,
			body:   "not valid json",
		},
		{
			name:   "GET method not allowed",
			method: http.MethodGet,
			body:   nil,
		},
		{
			name:   "PUT method not allowed",
			method: http.MethodPut,
			body:   Event{SubagentName: "test"},
		},
		{
			name:   "DELETE method not allowed",
			method: http.MethodDelete,
			body:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedEvent *Event
			var mu sync.Mutex
			handler := func(e Event) {
				mu.Lock()
				receivedEvent = &e
				mu.Unlock()
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			server := NewServer(handler)
			address, err := server.Start(ctx)
			if err != nil {
				t.Fatalf("failed to start server: %v", err)
			}

			var bodyBytes []byte
			if tt.body != nil {
				switch v := tt.body.(type) {
				case string:
					bodyBytes = []byte(v)
				default:
					bodyBytes, _ = json.Marshal(tt.body)
				}
			}

			req, err := http.NewRequest(tt.method, address+"/subagent-events", bytes.NewReader(bodyBytes))
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			mu.Lock()
			gotEvent := receivedEvent != nil
			mu.Unlock()

			result := struct {
				StatusCode    int
				EventReceived bool
			}{
				StatusCode:    resp.StatusCode,
				EventReceived: gotEvent,
			}
			cupaloy.SnapshotT(t, result)
		})
	}
}

func TestServerStartReturnsValidAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewServer(nil)
	address, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if address == "" {
		t.Error("address should not be empty")
	}

	// Verify address format
	if len(address) < 7 || address[:7] != "http://" {
		t.Errorf("address should start with http://, got %s", address)
	}

	// Verify server is reachable
	resp, err := http.Get(address + "/subagent-events")
	if err != nil {
		t.Fatalf("server not reachable: %v", err)
	}
	defer resp.Body.Close()
	// GET should return 405
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", resp.StatusCode)
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	server := NewServer(nil)
	address, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Verify server is running
	resp, err := http.Get(address + "/subagent-events")
	if err != nil {
		t.Fatalf("server not reachable before shutdown: %v", err)
	}
	resp.Body.Close()

	// Cancel context to trigger shutdown
	cancel()

	// Give shutdown time to complete
	time.Sleep(100 * time.Millisecond)

	// Server should no longer be reachable
	client := &http.Client{Timeout: 100 * time.Millisecond}
	_, err = client.Get(address + "/subagent-events")
	if err == nil {
		t.Error("server should not be reachable after shutdown")
	}
}

func TestServerEventHandlerReceivesCorrectData(t *testing.T) {
	expectedEvent := Event{
		SubagentName:            "my-agent",
		SubagentRunID:           "run-abc",
		Timestamp:               time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Type:                    EventTypeToolResult,
		ToolName:                "execute_code",
		ToolCallID:              "call-xyz",
		Payload:                 `{"result": "success"}`,
		ExecutionTimeoutSeconds: 30,
	}

	var receivedEvent Event
	var wg sync.WaitGroup
	wg.Add(1)

	handler := func(e Event) {
		receivedEvent = e
		wg.Done()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewServer(handler)
	address, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	bodyBytes, _ := json.Marshal(expectedEvent)
	resp, err := http.Post(address+"/subagent-events", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("failed to post event: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}

	wg.Wait()

	cupaloy.SnapshotT(t, receivedEvent)
}

func TestServerNilHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Server with nil handler should not panic
	server := NewServer(nil)
	address, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	event := Event{
		SubagentName:  "test",
		SubagentRunID: "run-1",
		Type:          EventTypeSubagentStart,
	}
	bodyBytes, _ := json.Marshal(event)
	resp, err := http.Post(address+"/subagent-events", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("failed to post event: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK with nil handler, got %d", resp.StatusCode)
	}
}

func TestServerConcurrentEventsNoInterleaving(t *testing.T) {
	const numGoroutines = 10
	const eventsPerGoroutine = 20

	var mu sync.Mutex
	var receivedEvents []Event

	handler := func(e Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, e)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewServer(handler)
	address, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Use errgroup-style pattern with WaitGroup
	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*eventsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				event := Event{
					SubagentName:  fmt.Sprintf("agent-%d", goroutineID),
					SubagentRunID: fmt.Sprintf("run-%d-%d", goroutineID, i),
					Type:          EventTypeToolCall,
					ToolName:      "test-tool",
					Payload:       fmt.Sprintf("payload from goroutine %d, event %d", goroutineID, i),
				}
				bodyBytes, _ := json.Marshal(event)
				resp, err := http.Post(address+"/subagent-events", "application/json", bytes.NewReader(bodyBytes))
				if err != nil {
					errChan <- fmt.Errorf("goroutine %d event %d: %w", goroutineID, i, err)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errChan <- fmt.Errorf("goroutine %d event %d: status %d", goroutineID, i, resp.StatusCode)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("request error: %v", err)
	}

	mu.Lock()
	eventCount := len(receivedEvents)
	mu.Unlock()

	expectedCount := numGoroutines * eventsPerGoroutine
	if eventCount != expectedCount {
		t.Errorf("received %d events, want %d", eventCount, expectedCount)
	}
}

func TestSyncWriterConcurrentWrites(t *testing.T) {
	const numGoroutines = 50
	const writesPerGoroutine = 100

	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				// Each write is a complete block with a marker
				block := fmt.Sprintf("[START:%d:%d]content-from-goroutine-%d-iteration-%d[END:%d:%d]\n", id, i, id, i, id, i)
				sw.WriteString(block)
			}
		}(g)
	}

	wg.Wait()

	output := buf.String()

	// Verify no interleaving: each block should be complete
	// Check that every START has a matching END on the same line
	lines := 0
	startIdx := 0
	for i := 0; i < len(output); i++ {
		if output[i] == '\n' {
			line := output[startIdx:i]
			lines++

			// Each line should start with [START: and end with ]
			if len(line) < 10 {
				t.Errorf("line %d too short: %q", lines, line)
				continue
			}
			if line[:7] != "[START:" {
				t.Errorf("line %d does not start with [START:: %q", lines, line[:min(20, len(line))])
			}
			// Extract the IDs from START and verify END matches
			// Format: [START:id:iter]...[END:id:iter]
			var startID, startIter int
			_, err := fmt.Sscanf(line, "[START:%d:%d]", &startID, &startIter)
			if err != nil {
				t.Errorf("line %d: failed to parse START: %v", lines, err)
				continue
			}

			expectedEnd := fmt.Sprintf("[END:%d:%d]", startID, startIter)
			if len(line) < len(expectedEnd) || line[len(line)-len(expectedEnd):] != expectedEnd {
				t.Errorf("line %d: expected to end with %q, got suffix %q", lines, expectedEnd, line[max(0, len(line)-30):])
			}
			startIdx = i + 1
		}
	}

	expectedLines := numGoroutines * writesPerGoroutine
	if lines != expectedLines {
		t.Errorf("got %d lines, want %d", lines, expectedLines)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
