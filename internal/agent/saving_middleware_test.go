package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/spachava753/gai"

	"github.com/spachava753/cpe/internal/types"
)

const testMsgID1 = "msg-1"

// mockDialogSaver is a test implementation of DialogSaver
type mockDialogSaver struct {
	savedMessages []savedMessage
	saveErr       error
	idCounter     int
}

type savedMessage struct {
	message  gai.Message
	parentID string
}

func (m *mockDialogSaver) SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
	if m.saveErr != nil {
		return "", m.saveErr
	}
	m.savedMessages = append(m.savedMessages, savedMessage{message: message, parentID: parentID})
	m.idCounter++
	return "msg-" + string(rune('0'+m.idCounter)), nil
}

// mockDialogSaverFunc allows custom save behavior via a function
type mockDialogSaverFunc struct {
	saveFunc func(ctx context.Context, message gai.Message, parentID string, label string) (string, error)
}

func (m *mockDialogSaverFunc) SaveMessage(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
	return m.saveFunc(ctx, message, parentID, label)
}

// mockInnerGenerator is a test implementation for the inner generator
type mockInnerGenerator struct {
	response gai.Response
	err      error
}

func (m *mockInnerGenerator) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	if m.err != nil {
		return gai.Response{}, m.err
	}
	return m.response, nil
}

// mockInnerGeneratorFunc allows custom generate behavior via a function
type mockInnerGeneratorFunc struct {
	generateFunc func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error)
}

func (m *mockInnerGeneratorFunc) Generate(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
	return m.generateFunc(ctx, dialog, opts)
}

func TestSavingMiddleware_SavesNewMessages(t *testing.T) {
	saver := &mockDialogSaver{}
	inner := &mockInnerGenerator{
		response: gai.Response{
			Candidates: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Hello!")},
					},
				},
			},
		},
	}

	middleware := NewSavingMiddleware(inner, saver)

	// Create a dialog with a new user message (no ID)
	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Hi there")},
			},
		},
	}

	resp, err := middleware.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Should have saved 2 messages: user and assistant
	if len(saver.savedMessages) != 2 {
		t.Errorf("Expected 2 saved messages, got %d", len(saver.savedMessages))
	}

	// Check the user message was saved with empty parent
	if saver.savedMessages[0].parentID != "" {
		t.Errorf("Expected empty parent for user message, got %q", saver.savedMessages[0].parentID)
	}

	// Check the assistant message was saved with user message as parent
	if saver.savedMessages[1].parentID != testMsgID1 {
		t.Errorf("Expected parent %q for assistant message, got %q", testMsgID1, saver.savedMessages[1].parentID)
	}

	// Check that the response candidate has the message ID set
	respID := GetMessageID(resp.Candidates[0])
	if respID != "msg-2" {
		t.Errorf("Expected response message ID 'msg-2', got %q", respID)
	}

	cupaloy.SnapshotT(t, saver.savedMessages)
}

func TestSavingMiddleware_SkipsAlreadySavedMessages(t *testing.T) {
	saver := &mockDialogSaver{}
	inner := &mockInnerGenerator{
		response: gai.Response{
			Candidates: []gai.Message{
				{
					Role: gai.Assistant,
					Blocks: []gai.Block{
						{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Response")},
					},
				},
			},
		},
	}

	middleware := NewSavingMiddleware(inner, saver)

	// Create a dialog with already-saved messages (have IDs)
	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("First message")},
			},
			ExtraFields: map[string]any{types.MessageIDKey: "existing-1"},
		},
		{
			Role: gai.Assistant,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("First response")},
			},
			ExtraFields: map[string]any{types.MessageIDKey: "existing-2"},
		},
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("New message")},
			},
			// No ID - should be saved
		},
	}

	_, err := middleware.Generate(context.Background(), dialog, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Should have saved 2 messages: the new user message and the assistant response
	if len(saver.savedMessages) != 2 {
		t.Errorf("Expected 2 saved messages, got %d", len(saver.savedMessages))
	}

	// The new user message should have the last saved message as parent
	if saver.savedMessages[0].parentID != "existing-2" {
		t.Errorf("Expected parent 'existing-2', got %q", saver.savedMessages[0].parentID)
	}
}

func TestSavingMiddleware_PropagatesGeneratorError(t *testing.T) {
	saver := &mockDialogSaver{}
	expectedErr := errors.New("generation failed")
	inner := &mockInnerGenerator{
		err: expectedErr,
	}

	middleware := NewSavingMiddleware(inner, saver)

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Hi")},
			},
		},
	}

	_, err := middleware.Generate(context.Background(), dialog, nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestSavingMiddleware_HandlesSaveErrorOnUserMessage(t *testing.T) {
	saveErr := errors.New("save failed")
	saver := &mockDialogSaver{saveErr: saveErr}
	inner := &mockInnerGenerator{
		response: gai.Response{
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: []gai.Block{{BlockType: gai.Content, Content: gai.Str("Hi")}}},
			},
		},
	}

	middleware := NewSavingMiddleware(inner, saver)

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Hi")},
			},
		},
	}

	_, err := middleware.Generate(context.Background(), dialog, nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, saveErr) {
		t.Errorf("Expected save error, got %v", err)
	}
}

func TestSavingMiddleware_ContextCancellation(t *testing.T) {
	t.Run("cancelled before save", func(t *testing.T) {
		saver := &mockDialogSaverFunc{
			saveFunc: func(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
				// Check if context is cancelled
				if err := ctx.Err(); err != nil {
					return "", err
				}
				return testMsgID1, nil
			},
		}

		inner := &mockInnerGenerator{
			response: gai.Response{
				Candidates: []gai.Message{
					{Role: gai.Assistant, Blocks: []gai.Block{{BlockType: gai.Content, Content: gai.Str("Hi")}}},
				},
			},
		}

		middleware := NewSavingMiddleware(inner, saver)

		// Create already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		dialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{{BlockType: gai.Content, Content: gai.Str("Hi")}}},
		}

		_, err := middleware.Generate(ctx, dialog, nil)
		if err == nil {
			t.Fatal("Expected error from cancelled context")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})

	t.Run("cancelled during inner generate", func(t *testing.T) {
		saveCount := 0
		saver := &mockDialogSaverFunc{
			saveFunc: func(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
				saveCount++
				return fmt.Sprintf("msg-%d", saveCount), nil
			},
		}

		inner := &mockInnerGeneratorFunc{
			generateFunc: func(ctx context.Context, dialog gai.Dialog, opts *gai.GenOpts) (gai.Response, error) {
				return gai.Response{}, context.Canceled
			},
		}

		middleware := NewSavingMiddleware(inner, saver)

		dialog := gai.Dialog{
			{Role: gai.User, Blocks: []gai.Block{{BlockType: gai.Content, Content: gai.Str("Hi")}}},
		}

		_, err := middleware.Generate(context.Background(), dialog, nil)
		if err == nil {
			t.Fatal("Expected error from cancelled context")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
		// User message should have been saved before inner generate
		if saveCount != 1 {
			t.Errorf("Expected 1 save call (user message), got %d", saveCount)
		}
	})
}

func TestSavingMiddleware_HandlesSaveErrorOnAssistantResponse(t *testing.T) {
	// Create a saver that succeeds on user message but fails on assistant response
	callCount := 0
	saveErr := errors.New("save failed on assistant")
	saver := &mockDialogSaverFunc{
		saveFunc: func(ctx context.Context, message gai.Message, parentID string, label string) (string, error) {
			callCount++
			if callCount == 1 {
				// First call (user message) succeeds
				return testMsgID1, nil
			}
			// Second call (assistant response) fails
			return "", saveErr
		},
	}

	inner := &mockInnerGenerator{
		response: gai.Response{
			Candidates: []gai.Message{
				{Role: gai.Assistant, Blocks: []gai.Block{{BlockType: gai.Content, Content: gai.Str("Response")}}},
			},
		},
	}

	middleware := NewSavingMiddleware(inner, saver)

	dialog := gai.Dialog{
		{
			Role: gai.User,
			Blocks: []gai.Block{
				{BlockType: gai.Content, ModalityType: gai.Text, Content: gai.Str("Hi")},
			},
		},
	}

	_, err := middleware.Generate(context.Background(), dialog, nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, saveErr) {
		t.Errorf("Expected save error on assistant, got %v", err)
	}

	// Verify both calls were made
	if callCount != 2 {
		t.Errorf("Expected 2 save calls, got %d", callCount)
	}
}

func TestGetMessageID(t *testing.T) {
	tests := []struct {
		name     string
		msg      gai.Message
		expected string
	}{
		{
			name:     "nil ExtraFields",
			msg:      gai.Message{},
			expected: "",
		},
		{
			name: "empty ExtraFields",
			msg: gai.Message{
				ExtraFields: map[string]any{},
			},
			expected: "",
		},
		{
			name: "has message ID",
			msg: gai.Message{
				ExtraFields: map[string]any{types.MessageIDKey: "test-id"},
			},
			expected: "test-id",
		},
		{
			name: "wrong type for ID",
			msg: gai.Message{
				ExtraFields: map[string]any{types.MessageIDKey: 123},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMessageID(tt.msg)
			if got != tt.expected {
				t.Errorf("GetMessageID() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSetMessageID(t *testing.T) {
	t.Run("sets ID on nil ExtraFields", func(t *testing.T) {
		msg := gai.Message{}
		SetMessageID(&msg, "new-id")

		if msg.ExtraFields == nil {
			t.Fatal("ExtraFields should not be nil")
		}
		if got := msg.ExtraFields[types.MessageIDKey]; got != "new-id" {
			t.Errorf("Expected ID 'new-id', got %v", got)
		}
	})

	t.Run("sets ID on existing ExtraFields", func(t *testing.T) {
		msg := gai.Message{
			ExtraFields: map[string]any{"other": "value"},
		}
		SetMessageID(&msg, "new-id")

		if got := msg.ExtraFields[types.MessageIDKey]; got != "new-id" {
			t.Errorf("Expected ID 'new-id', got %v", got)
		}
		if got := msg.ExtraFields["other"]; got != "value" {
			t.Errorf("Other field should be preserved")
		}
	})
}
