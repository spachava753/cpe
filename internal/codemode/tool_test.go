package codemode

import (
	"context"
	"testing"
)

func TestExecuteGoCodeCallback_RejectsNonPositiveExecutionTimeout(t *testing.T) {
	t.Parallel()

	callback := &ExecuteGoCodeCallback{MaxTimeout: 300}
	params := map[string]any{
		"code":             "package main\n",
		"executionTimeout": 0,
	}

	msg, callErr := callback.Call(context.Background(), params)
	if callErr != nil {
		t.Fatalf("unexpected callback error: %v", callErr)
	}
	if len(msg.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(msg.Blocks))
	}

	got := msg.Blocks[0].Content.String()
	want := "executionTimeout must be at least 1 second"
	if got != want {
		t.Fatalf("unexpected tool result: got %q want %q", got, want)
	}
}

func TestExecuteGoCodeCallback_RejectsExecutionTimeoutAboveConfiguredMax(t *testing.T) {
	t.Parallel()

	callback := &ExecuteGoCodeCallback{MaxTimeout: 10}
	params := map[string]any{
		"code":             "package main\n",
		"executionTimeout": 11,
	}

	msg, callErr := callback.Call(context.Background(), params)
	if callErr != nil {
		t.Fatalf("unexpected callback error: %v", callErr)
	}
	if len(msg.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(msg.Blocks))
	}

	got := msg.Blocks[0].Content.String()
	want := "executionTimeout exceeds maximum allowed (10 seconds)"
	if got != want {
		t.Fatalf("unexpected tool result: got %q want %q", got, want)
	}
}
