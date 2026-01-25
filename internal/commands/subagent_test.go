package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/cpe/internal/subagentlog"
	"github.com/spachava753/gai"
)

func TestExtractFinalAnswerParams(t *testing.T) {
	tests := []struct {
		name    string
		dialog  gai.Dialog
		wantErr bool
	}{
		{
			name: "final_answer called with simple params",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Please analyze this"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", FinalAnswerToolName, map[string]any{
							"summary": "This is the analysis result",
							"score":   42,
						}),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "final_answer with nested object",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Get review"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", FinalAnswerToolName, map[string]any{
							"review": map[string]any{
								"rating":  5,
								"comment": "Great!",
							},
						}),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "final_answer after other tool calls",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Do something"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", "other_tool", map[string]any{"arg": "value"}),
					},
				},
				{
					Role: gai.ToolResult,
					Blocks: []gai.Block{
						{ID: "tc1", BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("result")},
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc2", FinalAnswerToolName, map[string]any{
							"done": true,
						}),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "no final_answer called",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Do something"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						gai.TextBlock("Here is my response"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "only other tool calls, no final_answer",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Do something"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						mustToolCallBlock(t, "tc1", "other_tool", map[string]any{"arg": "value"}),
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractFinalAnswerParams(tt.dialog)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractFinalAnswerParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// Parse JSON for consistent ordering in snapshots
				var gotMap map[string]any
				if err := json.Unmarshal([]byte(got), &gotMap); err != nil {
					t.Fatalf("failed to parse got result as JSON: %v", err)
				}
				cupaloy.SnapshotT(t, gotMap)
			}
		})
	}
}

func TestExtractFinalResponse(t *testing.T) {
	tests := []struct {
		name   string
		dialog gai.Dialog
	}{
		{
			name: "simple text response",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Hello"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						gai.TextBlock("Hello! How can I help?"),
					},
				},
			},
		},
		{
			name: "multiple text blocks",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Hello"),
					},
				},
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						gai.TextBlock("First part"),
						gai.TextBlock("Second part"),
					},
				},
			},
		},
		{
			name:   "empty dialog",
			dialog: gai.Dialog{},
		},
		{
			name: "no assistant message",
			dialog: gai.Dialog{
				{
					Role: gai.User,
					Blocks: []gai.Block{
						gai.TextBlock("Hello"),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFinalResponse(tt.dialog)
			cupaloy.SnapshotT(t, got)
		})
	}
}

// mustToolCallBlock creates a tool call block for testing, panics on error
func mustToolCallBlock(t *testing.T, id, toolName string, params map[string]any) gai.Block {
	t.Helper()
	block, err := gai.ToolCallBlock(id, toolName, params)
	if err != nil {
		t.Fatalf("failed to create tool call block: %v", err)
	}
	return block
}

// --- Storage Persistence Tests ---

// subagentMockStorage is a mock storage for subagent tests that tracks parent IDs and labels
type subagentMockStorage struct {
	savedEntries []subagentSavedEntry
	idCounter    int
}

type subagentSavedEntry struct {
	msg      gai.Message
	parentID string
	label    string
	id       string
}

func (m *subagentMockStorage) GetMostRecentAssistantMessageId(ctx context.Context) (string, error) {
	return "", nil
}

func (m *subagentMockStorage) GetDialogForMessage(ctx context.Context, messageID string) (gai.Dialog, []string, error) {
	return nil, nil, nil
}

func (m *subagentMockStorage) SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
	id := fmt.Sprintf("msg_%d", m.idCounter)
	m.idCounter++
	m.savedEntries = append(m.savedEntries, subagentSavedEntry{
		msg:      message,
		parentID: parentID,
		label:    label,
		id:       id,
	})
	return id, nil
}

func (m *subagentMockStorage) Close() error {
	return nil
}

// subagentMockGenerator is a mock generator that returns configured responses
type subagentMockGenerator struct {
	responseBlocks []gai.Block
}

func (m *subagentMockGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	response := gai.Message{
		Role:   gai.Assistant,
		Blocks: m.responseBlocks,
	}
	return append(dialog, response), nil
}

// storageSnapshot captures storage entries in a snapshot-friendly format
type storageSnapshot struct {
	Role     string `json:"role"`
	ParentID string `json:"parentID"`
	Label    string `json:"label"`
	ID       string `json:"id"`
}

func toStorageSnapshots(entries []subagentSavedEntry) []storageSnapshot {
	result := make([]storageSnapshot, len(entries))
	for i, e := range entries {
		result[i] = storageSnapshot{
			Role:     e.msg.Role.String(),
			ParentID: e.parentID,
			Label:    e.label,
			ID:       e.id,
		}
	}
	return result
}

func TestExecuteSubagent_PersistsToStorage(t *testing.T) {
	storage := &subagentMockStorage{}
	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("Hello from the subagent!"),
			},
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:    userBlocks,
		Generator:     generator,
		Storage:       storage,
		SubagentLabel: "subagent:test_agent:abc123",
	})

	if err != nil {
		t.Fatalf("ExecuteSubagent failed: %v", err)
	}

	cupaloy.SnapshotT(t, map[string]any{
		"result":         result,
		"storageEntries": toStorageSnapshots(storage.savedEntries),
	})
}

func TestExecuteSubagent_NoStorageNoError(t *testing.T) {
	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("Response without storage"),
			},
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	// Execute without storage - should still work
	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks: userBlocks,
		Generator:  generator,
		// No storage set
	})

	if err != nil {
		t.Fatalf("ExecuteSubagent failed: %v", err)
	}

	cupaloy.SnapshotT(t, result)
}

func TestExecuteSubagent_MultipleAssistantMessages(t *testing.T) {
	storage := &subagentMockStorage{}

	// Simulate a generator that returns multiple assistant messages (e.g., tool call + response)
	generator := &subagentMultiMsgGenerator{
		responses: []gai.Message{
			{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.ToolCall,
						ModalityType: gai.Text,
						Content:      gai.Str(`{"name":"test_tool","parameters":{}}`),
					},
				},
			},
			{
				Role: gai.ToolResult,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("tool result"),
					},
				},
			},
			{
				Role: gai.Assistant,
				Blocks: []gai.Block{
					{
						BlockType:    gai.Content,
						ModalityType: gai.Text,
						Content:      gai.Str("Final response"),
					},
				},
			},
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:    userBlocks,
		Generator:     generator,
		Storage:       storage,
		SubagentLabel: "subagent:multi:xyz789",
	})

	if err != nil {
		t.Fatalf("ExecuteSubagent failed: %v", err)
	}

	cupaloy.SnapshotT(t, map[string]any{
		"result":         result,
		"storageEntries": toStorageSnapshots(storage.savedEntries),
	})
}

// subagentMultiMsgGenerator returns multiple messages in the dialog
type subagentMultiMsgGenerator struct {
	responses []gai.Message
}

func (m *subagentMultiMsgGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	return append(dialog, m.responses...), nil
}

// --- Context and Error Handling Tests ---

// subagentContextAwareGenerator respects context cancellation
type subagentContextAwareGenerator struct {
	generateFunc func(ctx context.Context) (gai.Dialog, error)
}

func (m *subagentContextAwareGenerator) Generate(ctx context.Context, dialog gai.Dialog, optsGen gai.GenOptsGenerator) (gai.Dialog, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return dialog, nil
	}
}

func TestExecuteSubagent_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	generator := &subagentContextAwareGenerator{
		generateFunc: func(ctx context.Context) (gai.Dialog, error) {
			return nil, ctx.Err()
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	_, err := ExecuteSubagent(ctx, SubagentOptions{
		UserBlocks: userBlocks,
		Generator:  generator,
	})

	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "generation failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecuteSubagent_EmptyInput(t *testing.T) {
	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("Response"),
			},
		},
	}

	_, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks: nil, // Empty input
		Generator:  generator,
	})

	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "empty input") {
		t.Errorf("expected 'empty input' error, got: %v", err)
	}
}

func TestExecuteSubagent_GenerationError(t *testing.T) {
	expectedErr := fmt.Errorf("API rate limit exceeded")
	generator := &subagentContextAwareGenerator{
		generateFunc: func(ctx context.Context) (gai.Dialog, error) {
			return nil, expectedErr
		},
	}

	userBlocks := []gai.Block{
		{
			BlockType:    gai.Content,
			ModalityType: gai.Text,
			Content:      gai.Str("Test prompt"),
		},
	}

	_, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks: userBlocks,
		Generator:  generator,
	})

	if err == nil {
		t.Fatal("expected error for generation failure")
	}
	if !strings.Contains(err.Error(), "generation failed") {
		t.Errorf("expected 'generation failed' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected original error to be wrapped, got: %v", err)
	}
}

// --- Event Emission Error Handling Tests ---

func TestExecuteSubagent_SubagentStartEmissionFailureAbortsImmediately(t *testing.T) {
	// Create a server that always returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	eventClient := subagentlog.NewClient(server.URL)

	// Use a generator that should NOT be called if start emission fails
	generatorCalled := false
	generator := &subagentContextAwareGenerator{
		generateFunc: func(ctx context.Context) (gai.Dialog, error) {
			generatorCalled = true
			return gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("response")},
					},
				},
			}, nil
		},
	}

	userBlocks := []gai.Block{
		{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Test prompt")},
	}

	_, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:   userBlocks,
		Generator:    generator,
		EventClient:  eventClient,
		SubagentName: "test_subagent",
		RunID:        "run_123",
	})

	if err == nil {
		t.Fatal("expected error when subagent_start emission fails, got nil")
	}

	// Verify error message is descriptive
	errMsg := err.Error()
	if !strings.Contains(errMsg, "subagent_start") {
		t.Errorf("error message should mention 'subagent_start', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "500") {
		t.Errorf("error message should mention status code 500, got: %s", errMsg)
	}

	// Verify generator was NOT called (fail-fast)
	if generatorCalled {
		t.Error("generator should not be called when subagent_start emission fails")
	}
}

func TestExecuteSubagent_ConnectionRefusedAbortsImmediately(t *testing.T) {
	// Use an address that will definitely refuse connection
	eventClient := subagentlog.NewClient("http://127.0.0.1:1")

	generatorCalled := false
	generator := &subagentContextAwareGenerator{
		generateFunc: func(ctx context.Context) (gai.Dialog, error) {
			generatorCalled = true
			return gai.Dialog{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("response")},
					},
				},
			}, nil
		},
	}

	userBlocks := []gai.Block{
		{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Test prompt")},
	}

	_, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:   userBlocks,
		Generator:    generator,
		EventClient:  eventClient,
		SubagentName: "test_subagent",
		RunID:        "run_123",
	})

	if err == nil {
		t.Fatal("expected error when connection refused, got nil")
	}

	// Verify error message mentions connection failure
	errMsg := err.Error()
	if !strings.Contains(errMsg, "subagent_start") {
		t.Errorf("error message should mention 'subagent_start', got: %s", errMsg)
	}

	// Verify generator was NOT called (fail-fast)
	if generatorCalled {
		t.Error("generator should not be called when event emission connection fails")
	}
}

func TestExecuteSubagent_EventEmissionDuringGenerationAbortsExecution(t *testing.T) {
	// Track requests to alternate behavior
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			// Allow subagent_start to succeed
			w.WriteHeader(http.StatusOK)
		} else {
			// Fail subsequent events (thinking/tool_call during generation)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	eventClient := subagentlog.NewClient(server.URL)

	// Generator that produces a thinking block (which triggers emission)
	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Thinking,
				ModalityType: gai.Text,
				Content:      gai.Str("thinking about the problem..."),
			},
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("response"),
			},
		},
	}

	userBlocks := []gai.Block{
		{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Test prompt")},
	}

	_, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:   userBlocks,
		Generator:    generator,
		EventClient:  eventClient,
		SubagentName: "test_subagent",
		RunID:        "run_123",
	})

	if err == nil {
		t.Fatal("expected error when event emission during generation fails, got nil")
	}

	// Verify error message mentions emission failure
	errMsg := err.Error()
	if !strings.Contains(errMsg, "generation failed") || !strings.Contains(errMsg, "emit") {
		t.Errorf("error message should mention generation failure and emit, got: %s", errMsg)
	}
}

// eventSnapshot captures event data in a snapshot-friendly format
type eventSnapshot struct {
	Type          string `json:"type"`
	SubagentName  string `json:"subagentName"`
	SubagentRunID string `json:"subagentRunID"`
}

func toEventSnapshots(events []subagentlog.Event) []eventSnapshot {
	result := make([]eventSnapshot, len(events))
	for i, e := range events {
		result[i] = eventSnapshot{
			Type:          string(e.Type),
			SubagentName:  e.SubagentName,
			SubagentRunID: e.SubagentRunID,
		}
	}
	return result
}

func TestExecuteSubagent_SuccessfulEventEmission(t *testing.T) {
	// Track received events
	var receivedEvents []subagentlog.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event subagentlog.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err == nil {
			receivedEvents = append(receivedEvents, event)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	eventClient := subagentlog.NewClient(server.URL)

	generator := &subagentMockGenerator{
		responseBlocks: []gai.Block{
			{
				BlockType:    gai.Content,
				ModalityType: gai.Text,
				Content:      gai.Str("Hello!"),
			},
		},
	}

	userBlocks := []gai.Block{
		{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Test prompt")},
	}

	result, err := ExecuteSubagent(context.Background(), SubagentOptions{
		UserBlocks:   userBlocks,
		Generator:    generator,
		EventClient:  eventClient,
		SubagentName: "test_subagent",
		RunID:        "run_123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cupaloy.SnapshotT(t, map[string]any{
		"result": result,
		"events": toEventSnapshots(receivedEvents),
	})
}
