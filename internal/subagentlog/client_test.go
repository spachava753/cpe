package subagentlog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_Emit(t *testing.T) {
	testEvent := Event{
		SubagentName:  "test-agent",
		SubagentRunID: "run-123",
		Timestamp:     time.Now(),
		Type:          EventTypeToolCall,
		ToolName:      "test_tool",
		ToolCallID:    "call-456",
		Payload:       "test payload",
	}

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		address    string
		wantErr    bool
		errContain string
	}{
		{
			name: "successful emission",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST method, got %s", r.Method)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected application/json content type, got %s", r.Header.Get("Content-Type"))
				}

				var received Event
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Errorf("failed to decode event: %v", err)
				}
				if received.SubagentName != testEvent.SubagentName {
					t.Errorf("expected subagent name %s, got %s", testEvent.SubagentName, received.SubagentName)
				}

				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
		{
			name: "server returns 400 Bad Request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Bad Request", http.StatusBadRequest)
			},
			wantErr:    true,
			errContain: "non-2xx status: 400",
		},
		{
			name: "server returns 500 Internal Server Error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "non-2xx status: 500",
		},
		{
			name: "server returns 201 Created",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			},
			wantErr: false,
		},
		{
			name:       "connection refused",
			handler:    nil,
			address:    "http://127.0.0.1:1",
			wantErr:    true,
			errContain: "failed to send event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var address string
			if tt.handler != nil {
				server := httptest.NewServer(tt.handler)
				defer server.Close()
				address = server.URL
			} else {
				address = tt.address
			}

			client := NewClient(address)
			err := client.Emit(context.Background(), testEvent)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("expected error containing %q, got %q", tt.errContain, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestClient_Emit_Timeout(t *testing.T) {
	var handlerStarted atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerStarted.Store(true)
		// Sleep for longer than the context timeout, but use select to handle context cancellation
		select {
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Request was cancelled, just return
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.Emit(ctx, Event{
		SubagentName:  "test-agent",
		SubagentRunID: "run-123",
		Timestamp:     time.Now(),
		Type:          EventTypeToolCall,
	})

	if err == nil {
		t.Error("expected timeout error, got nil")
	}

	// Verify the handler was actually reached
	if !handlerStarted.Load() {
		t.Error("handler was never called")
	}
}

func TestClient_Emit_ContextCancellation(t *testing.T) {
	var handlerStarted atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerStarted.Store(true)
		select {
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Request was cancelled
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := client.Emit(ctx, Event{
		SubagentName:  "test-agent",
		SubagentRunID: "run-123",
		Timestamp:     time.Now(),
		Type:          EventTypeToolCall,
	})

	if err == nil {
		t.Error("expected cancellation error, got nil")
	}
}

func TestClient_Reusable(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	for i := 0; i < 3; i++ {
		err := client.Emit(context.Background(), Event{
			SubagentName:  "test-agent",
			SubagentRunID: "run-123",
			Timestamp:     time.Now(),
			Type:          EventTypeToolCall,
		})
		if err != nil {
			t.Errorf("emission %d failed: %v", i, err)
		}
	}

	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}
