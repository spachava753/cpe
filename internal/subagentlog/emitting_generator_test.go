package subagentlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/codemode"
)

// mockGenerator implements the generator interface for testing
type mockGenerator struct {
	generateFunc func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error)
	registerFunc func(tool gai.Tool, callback gai.ToolCallback) error
}

func (m *mockGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, dialog, optsGen)
	}
	return dialog, nil
}

func (m *mockGenerator) Register(tool gai.Tool, callback gai.ToolCallback) error {
	if m.registerFunc != nil {
		return m.registerFunc(tool, callback)
	}
	return nil
}

// mockToolCallback implements gai.ToolCallback for testing
type mockToolCallback struct {
	callFunc func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error)
}

func (m *mockToolCallback) Call(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
	if m.callFunc != nil {
		return m.callFunc(ctx, parametersJSON, toolCallID)
	}
	return gai.Message{
		Role: gai.ToolResult,
		Blocks: []gai.Block{
			{
				ID:           toolCallID,
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				MimeType:     "text/plain",
				Content:      gai.Str("mock result"),
			},
		},
	}, nil
}

// normalizedEvent is a copy of Event with the Timestamp field zeroed for snapshot testing
type normalizedEvent struct {
	Type                    string
	SubagentName            string
	SubagentRunID           string
	ToolName                string
	ToolCallID              string
	Payload                 string
	ReasoningType           string
	ExecutionTimeoutSeconds int
}

// normalizeEvents creates a copy of events with timestamps zeroed for deterministic snapshots
func normalizeEvents(events []Event) []normalizedEvent {
	normalized := make([]normalizedEvent, len(events))
	for i, e := range events {
		normalized[i] = normalizedEvent{
			Type:                    e.Type,
			SubagentName:            e.SubagentName,
			SubagentRunID:           e.SubagentRunID,
			ToolName:                e.ToolName,
			ToolCallID:              e.ToolCallID,
			Payload:                 e.Payload,
			ReasoningType:           e.ReasoningType,
			ExecutionTimeoutSeconds: e.ExecutionTimeoutSeconds,
		}
	}
	return normalized
}

// createTestServer creates a test HTTP server that records events
func createTestServer(t *testing.T) (*httptest.Server, *[]Event, *sync.Mutex) {
	t.Helper()
	var events []Event
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subagent-events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Errorf("failed to decode event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		events = append(events, event)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))

	return server, &events, &mu
}

// TestEmittingToolCallback_ToolCallAndResultEvents tests that tool_call is emitted before tool_result
func TestEmittingToolCallback_ToolCallAndResultEvents(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	baseCallback := &mockToolCallback{
		callFunc: func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			return gai.Message{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						ID:           toolCallID,
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("tool execution result"),
					},
				},
			}, nil
		},
	}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_789", "my_tool")

	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{"key":"value"}`), "call_abc")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*events) != 2 {
		t.Fatalf("expected 2 events (tool_call + tool_result), got %d", len(*events))
	}

	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

// TestEmittingToolCallback_ExecuteGoCodeToolCallEvent tests special handling of execute_go_code
func TestEmittingToolCallback_ExecuteGoCodeToolCallEvent(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	goCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`

	params := map[string]any{
		"code":             goCode,
		"executionTimeout": float64(120),
	}
	paramsJSON, _ := json.Marshal(params)

	baseCallback := &mockToolCallback{
		callFunc: func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			return gai.Message{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						ID:           toolCallID,
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("Hello, World!"),
					},
				},
			}, nil
		},
	}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_code", codemode.ExecuteGoCodeToolName)

	_, err := emittingCallback.Call(context.Background(), paramsJSON, "call_code")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(*events))
	}

	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

func TestEmittingGenerator_FinalAnswerToolCallSkipped(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	// Create a final_answer tool call input
	toolCallInput := gai.ToolCallInput{
		Name:       "final_answer",
		Parameters: map[string]any{"result": "done"},
	}
	toolCallJSON, _ := json.Marshal(toolCallInput)

	baseGen := &mockGenerator{
		generateFunc: func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
			// Return dialog with a final_answer tool call block
			return append(dialog, gai.Message{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						ID:           "call_final",
						BlockType:    gai.ToolCall,
						ModalityType: gai.Text,
						Content:      gai.Str(string(toolCallJSON)),
					},
				},
			}), nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_123")

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
			},
		},
	}

	_, err := emittingGen.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// No events should be emitted for final_answer tool calls
	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

func TestEmittingToolCallback_FinalAnswerSkipped(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	baseCallback := &mockToolCallback{
		callFunc: func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			return gai.Message{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						ID:           toolCallID,
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("final result"),
					},
				},
			}, nil
		},
	}

	// Create callback for final_answer tool
	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_123", "final_answer")

	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{"result":"done"}`), "call_final")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// No events should be emitted for final_answer
	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

func TestEmittingGenerator_ThoughtTraceEvent(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	baseGen := &mockGenerator{
		generateFunc: func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
			// Return dialog with a thinking block
			return append(dialog, gai.Message{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Thinking,
						ModalityType: gai.Text,
						Content:      gai.Str("thinking about the problem..."),
						ExtraFields: map[string]interface{}{
							"reasoning_type": "reasoning.text",
						},
					},
				},
			}), nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_456")

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
			},
		},
	}

	_, err := emittingGen.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(*events))
	}

	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

func TestEmittingGenerator_EmissionFailureAbortsExecution(t *testing.T) {
	// Create a server that always returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	baseGen := &mockGenerator{
		generateFunc: func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
			// Return dialog with a thinking block that will trigger event emission
			return append(dialog, gai.Message{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Thinking,
						ModalityType: gai.Text,
						Content:      gai.Str("thinking..."),
					},
				},
			}), nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_123")

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
			},
		},
	}

	_, err := emittingGen.Generate(context.Background(), dialog, nil)
	if err == nil {
		t.Fatal("expected error when emission fails, got nil")
	}
	if !errors.Is(err, nil) {
		// Just check that the error message mentions event emission
		if err.Error() == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestEmittingToolCallback_EmissionFailureAbortsExecution(t *testing.T) {
	// Create a server that always returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	baseCallback := &mockToolCallback{}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_123", "my_tool")

	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{}`), "call_123")
	if err == nil {
		t.Fatal("expected error when emission fails, got nil")
	}
}

func TestEmittingGenerator_Register(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	var registeredCallback gai.ToolCallback
	baseGen := &mockGenerator{
		registerFunc: func(tool gai.Tool, callback gai.ToolCallback) error {
			registeredCallback = callback
			return nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_123")

	// Register a tool with a callback
	originalCallback := &mockToolCallback{}
	err := emittingGen.Register(gai.Tool{Name: "test_tool"}, originalCallback)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify the callback was wrapped
	if registeredCallback == nil {
		t.Fatal("expected callback to be registered")
	}

	// The registered callback should be an EmittingToolCallback
	_, ok := registeredCallback.(*EmittingToolCallback)
	if !ok {
		t.Error("expected callback to be wrapped with EmittingToolCallback")
	}

	// Call the registered callback and verify events are emitted
	_, err = registeredCallback.Call(context.Background(), json.RawMessage(`{}`), "call_xyz")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*events) != 2 {
		t.Fatalf("expected 2 events (tool_call + tool_result), got %d", len(*events))
	}

	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

func TestEmittingGenerator_RegisterNilCallback(t *testing.T) {
	server, _, _ := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	var registeredCallback gai.ToolCallback
	baseGen := &mockGenerator{
		registerFunc: func(tool gai.Tool, callback gai.ToolCallback) error {
			registeredCallback = callback
			return nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_123")

	// Register a tool with nil callback (used for termination tools)
	err := emittingGen.Register(gai.Tool{Name: "final_answer"}, nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify nil callback was passed through
	if registeredCallback != nil {
		t.Error("expected nil callback to be passed through")
	}
}

func TestEmittingToolCallback_ToolCallEmissionFailureAbortsExecution(t *testing.T) {
	// Create a server that always returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	baseCallback := &mockToolCallback{}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_123", "test_tool")

	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{"param1":"value1"}`), "call_123")
	if err == nil {
		t.Fatal("expected error when tool_call emission fails, got nil")
	}

	// Verify error message is descriptive
	errMsg := err.Error()
	if !strings.Contains(errMsg, "tool_call") {
		t.Errorf("error message should mention 'tool_call', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "non-2xx") || !strings.Contains(errMsg, "500") {
		t.Errorf("error message should mention status code, got: %s", errMsg)
	}
}

func TestEmittingGenerator_ThoughtTraceEmissionFailureHasDescriptiveError(t *testing.T) {
	// Create a server that returns 503 Service Unavailable
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	baseGen := &mockGenerator{
		generateFunc: func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
			return append(dialog, gai.Message{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Thinking,
						ModalityType: gai.Text,
						Content:      gai.Str("thinking..."),
					},
				},
			}), nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_123")

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
			},
		},
	}

	_, err := emittingGen.Generate(context.Background(), dialog, nil)
	if err == nil {
		t.Fatal("expected error when thought_trace emission fails, got nil")
	}

	// Verify error message is descriptive
	errMsg := err.Error()
	if !strings.Contains(errMsg, "thought_trace") {
		t.Errorf("error message should mention 'thought_trace', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "non-2xx") || !strings.Contains(errMsg, "503") {
		t.Errorf("error message should mention status code 503, got: %s", errMsg)
	}
}

func TestEmittingGenerator_ConnectionRefusedAbortsExecution(t *testing.T) {
	// Use an address that will definitely refuse connection
	client := NewClient("http://127.0.0.1:1")

	baseGen := &mockGenerator{
		generateFunc: func(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
			return append(dialog, gai.Message{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Thinking,
						ModalityType: gai.Text,
						Content:      gai.Str("thinking..."),
					},
				},
			}), nil
		},
	}

	emittingGen := NewEmittingGenerator(baseGen, client, "test_subagent", "run_123")

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
			},
		},
	}

	_, err := emittingGen.Generate(context.Background(), dialog, nil)
	if err == nil {
		t.Fatal("expected error when connection refused, got nil")
	}

	// Verify error message mentions connection failure
	errMsg := err.Error()
	if !strings.Contains(errMsg, "failed to emit") {
		t.Errorf("error message should mention 'failed to emit', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "failed to send event") {
		t.Errorf("error message should mention 'failed to send event', got: %s", errMsg)
	}
}

func TestEmittingToolCallback_ToolResultEmissionFailureHasDescriptiveError(t *testing.T) {
	// Create a server that succeeds for tool_call but fails for tool_result
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call (tool_call) succeeds
			w.WriteHeader(http.StatusOK)
		} else {
			// Second call (tool_result) fails
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)

	baseCallback := &mockToolCallback{
		callFunc: func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			return gai.Message{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						ID:           toolCallID,
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("tool execution result"),
					},
				},
			}, nil
		},
	}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_123", "my_tool")

	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{}`), "call_123")
	if err == nil {
		t.Fatal("expected error when emission fails, got nil")
	}

	// Verify error message is descriptive
	errMsg := err.Error()
	if !strings.Contains(errMsg, "tool_result") {
		t.Errorf("error message should mention 'tool_result', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "non-2xx") || !strings.Contains(errMsg, "502") {
		t.Errorf("error message should mention status code 502, got: %s", errMsg)
	}
}

func TestEmittingToolCallback_ConnectionRefusedAbortsExecution(t *testing.T) {
	// Use an address that will definitely refuse connection
	client := NewClient("http://127.0.0.1:1")

	baseCallback := &mockToolCallback{}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test_subagent", "run_123", "my_tool")

	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{}`), "call_123")
	if err == nil {
		t.Fatal("expected error when connection refused, got nil")
	}

	// Verify error message mentions connection failure
	errMsg := err.Error()
	if !strings.Contains(errMsg, "failed to emit") {
		t.Errorf("error message should mention 'failed to emit', got: %s", errMsg)
	}
}

// TestEventOrdering verifies that tool_call events are emitted before tool_result events
func TestEventOrdering(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	baseCallback := &mockToolCallback{
		callFunc: func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			return gai.Message{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						ID:           toolCallID,
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("result"),
					},
				},
			}, nil
		},
	}

	emittingCallback := NewEmittingToolCallback(baseCallback, client, "test", "run", "tool")

	// Call the callback
	_, err := emittingCallback.Call(context.Background(), json.RawMessage(`{"arg":"val"}`), "call_1")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(*events))
	}

	// Verify timestamps are in order (keep this check since it's about ordering, not values)
	if (*events)[1].Timestamp.Before((*events)[0].Timestamp) {
		t.Error("tool_result timestamp should not be before tool_call timestamp")
	}

	cupaloy.SnapshotT(t, normalizeEvents(*events))
}

// mockToolCapableGenerator implements gai.ToolCapableGenerator for testing the inner wrapper
type mockToolCapableGenerator struct {
	generateFunc func(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error)
	registerFunc func(tool gai.Tool) error
}

func (m *mockToolCapableGenerator) Generate(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, dialog, options)
	}
	return gai.Response{}, nil
}

func (m *mockToolCapableGenerator) Register(tool gai.Tool) error {
	if m.registerFunc != nil {
		return m.registerFunc(tool)
	}
	return nil
}

// TestEmittingGenerator_ThinkingBeforeToolCall verifies that when using a *gai.ToolGenerator,
// thinking events are emitted BEFORE tool call events
func TestEmittingGenerator_ThinkingBeforeToolCall(t *testing.T) {
	server, events, mu := createTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)

	// Track the order of events
	var callOrder []string
	var callMu sync.Mutex

	// Create a mock inner generator that returns thinking + tool call
	mockInner := &mockToolCapableGenerator{
		generateFunc: func(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
			callMu.Lock()
			callOrder = append(callOrder, "generate_called")
			callMu.Unlock()

			toolCallInput := gai.ToolCallInput{
				Name:       "test_tool",
				Parameters: map[string]any{"arg": "value"},
			}
			toolCallJSON, _ := json.Marshal(toolCallInput)

			return gai.Response{
				Candidates: []gai.Message{
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								Content:      gai.Str("Let me think about this..."),
							},
							{
								ID:           "call_123",
								BlockType:    gai.ToolCall,
								ModalityType: gai.Text,
								Content:      gai.Str(string(toolCallJSON)),
							},
						},
					},
				},
				FinishReason: gai.ToolUse,
			}, nil
		},
	}

	// Create a real ToolGenerator with our mock inner
	toolGen := &gai.ToolGenerator{
		G: mockInner,
	}

	// Create EmittingGenerator - this should wrap the inner generator
	emittingGen := NewEmittingGenerator(toolGen, client, "test_subagent", "run_ordering")

	// Register a tool that will be called
	toolCallback := &mockToolCallback{
		callFunc: func(ctx context.Context, parametersJSON json.RawMessage, toolCallID string) (gai.Message, error) {
			callMu.Lock()
			callOrder = append(callOrder, "callback_executed")
			callMu.Unlock()

			return gai.Message{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						ID:           toolCallID,
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						MimeType:     "text/plain",
						Content:      gai.Str("tool result"),
					},
				},
			}, nil
		},
	}

	if err := emittingGen.Register(gai.Tool{Name: "test_tool"}, toolCallback); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Set up the mock to return end turn after tool result
	callCount := 0
	mockInner.generateFunc = func(ctx context.Context, dialog gai.Dialog, options *gai.GenOpts) (gai.Response, error) {
		callMu.Lock()
		callCount++
		count := callCount
		callOrder = append(callOrder, fmt.Sprintf("generate_called_%d", count))
		callMu.Unlock()

		if count == 1 {
			// First call: return thinking + tool call
			toolCallInput := gai.ToolCallInput{
				Name:       "test_tool",
				Parameters: map[string]any{"arg": "value"},
			}
			toolCallJSON, _ := json.Marshal(toolCallInput)

			return gai.Response{
				Candidates: []gai.Message{
					{
						Role: gai.Assistant,
						Blocks: []gai.Block{
							{
								BlockType:    gai.Thinking,
								ModalityType: gai.Text,
								Content:      gai.Str("Let me think about this..."),
							},
							{
								ID:           "call_123",
								BlockType:    gai.ToolCall,
								ModalityType: gai.Text,
								Content:      gai.Str(string(toolCallJSON)),
							},
						},
					},
				},
				FinishReason: gai.ToolUse,
			}, nil
		}

		// Second call: return final response (end turn)
		return gai.Response{
			Candidates: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{
							BlockType:    gai.Content,
							ModalityType: gai.Text,
							Content:      gai.Str("Done!"),
						},
					},
				},
			},
			FinishReason: gai.EndTurn,
		}, nil
	}

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("test")},
			},
		},
	}

	_, err := emittingGen.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(*events) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(*events), *events)
	}

	// Find the indices of different event types to verify ordering
	var thoughtIdx, toolCallIdx, toolResultIdx = -1, -1, -1
	for i, e := range *events {
		switch e.Type {
		case EventTypeThoughtTrace:
			if thoughtIdx == -1 {
				thoughtIdx = i
			}
		case EventTypeToolCall:
			if toolCallIdx == -1 {
				toolCallIdx = i
			}
		case EventTypeToolResult:
			if toolResultIdx == -1 {
				toolResultIdx = i
			}
		}
	}

	// Keep ordering verification since it's about relative ordering
	if thoughtIdx == -1 {
		t.Error("thought_trace event not found")
	}
	if toolCallIdx == -1 {
		t.Error("tool_call event not found")
	}
	if toolResultIdx == -1 {
		t.Error("tool_result event not found")
	}

	if thoughtIdx != -1 && toolCallIdx != -1 && thoughtIdx >= toolCallIdx {
		t.Errorf("thought_trace (index %d) should appear before tool_call (index %d)", thoughtIdx, toolCallIdx)
	}
	if toolCallIdx != -1 && toolResultIdx != -1 && toolCallIdx >= toolResultIdx {
		t.Errorf("tool_call (index %d) should appear before tool_result (index %d)", toolCallIdx, toolResultIdx)
	}

	// Verify timestamps are in order
	if thoughtIdx != -1 && toolCallIdx != -1 {
		if (*events)[toolCallIdx].Timestamp.Before((*events)[thoughtIdx].Timestamp) {
			t.Error("tool_call timestamp should not be before thought_trace timestamp")
		}
	}

	cupaloy.SnapshotT(t, normalizeEvents(*events))
}
